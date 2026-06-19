// Package llmgateway is the default semantic-cache embedding generator: it
// routes embedding requests through the Portico LLM gateway (engine + alias
// resolver) so the operator's Portico-managed provider credentials apply, and
// so a dedicated cheap "cache embedding model" alias can be used. Semantic cache
// drivers (weaviate, qdrant) call it via the ifaces.EmbeddingGenerator seam.
package llmgateway

import (
	"context"
	"errors"
	"fmt"

	embeddingifaces "github.com/hurtener/Portico_gateway/internal/llm/cache/embeddings/ifaces"
	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Deps are the gateway pieces the generator needs: the inference engine, the
// tenant-scoped alias→model and provider stores, and the model alias to embed
// with (the operator's configured "cache embedding model").
type Deps struct {
	Engine    engineifaces.Engine
	Models    storageifaces.LLMModelStore
	Providers storageifaces.LLMProviderStore
	Alias     string
}

// ErrNotConfigured is returned when the generator lacks the engine/stores/alias
// needed to embed (semantic callers then degrade to a cache miss).
var ErrNotConfigured = errors.New("cache: embedding generator not configured")

type generator struct {
	deps Deps
}

// New builds the LLM-gateway-backed embedding generator. It is safe to construct
// with a zero alias / nil engine — Embed then returns ErrNotConfigured so the
// caller degrades gracefully rather than failing the request.
func New(deps Deps) embeddingifaces.EmbeddingGenerator { return &generator{deps: deps} }

func (g *generator) Name() string { return "llm_gateway" }

// Embed resolves the cache-embedding alias to a provider+model (tenant-scoped)
// and runs the inputs through the engine, returning one float32 vector per
// input. The engine returns float64 vectors; we narrow to float32 (the cache
// vector type) — embedding magnitudes are well within float32 range.
func (g *generator) Embed(ctx context.Context, tenantID string, input []string) ([][]float32, error) {
	if g.deps.Engine == nil || g.deps.Models == nil || g.deps.Providers == nil || g.deps.Alias == "" {
		return nil, ErrNotConfigured
	}
	if len(input) == 0 {
		return nil, nil
	}
	model, err := g.deps.Models.GetModel(ctx, tenantID, g.deps.Alias)
	if err != nil {
		return nil, fmt.Errorf("cache embed: resolve alias %q: %w", g.deps.Alias, err)
	}
	prov, err := g.deps.Providers.GetProvider(ctx, tenantID, model.ProviderName)
	if err != nil {
		return nil, fmt.Errorf("cache embed: resolve provider: %w", err)
	}
	resp, err := g.deps.Engine.Embedding(ctx, &engineifaces.EmbeddingRequest{
		TenantID:      tenantID,
		Provider:      prov.Driver,
		ProviderModel: model.ProviderModel,
		Input:         input,
	})
	if err != nil {
		return nil, fmt.Errorf("cache embed: engine: %w", err)
	}
	out := make([][]float32, len(resp.Embeddings))
	for i, v := range resp.Embeddings {
		f := make([]float32, len(v))
		for j, x := range v {
			f[j] = float32(x)
		}
		out[i] = f
	}
	return out, nil
}
