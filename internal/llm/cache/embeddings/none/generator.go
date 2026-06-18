// Package none is a no-op embedding generator. Semantic cache lookups that
// require an embedding degrade gracefully to a miss when this generator is in
// use; exact-hash caching is unaffected.
package none

import (
	"context"
	"errors"

	embeddingifaces "github.com/hurtener/Portico_gateway/internal/llm/cache/embeddings/ifaces"
)

// ErrNoEmbedder signals that no embedding model is configured; semantic mode
// callers treat this as "no vector available" and fall back to exact/miss.
var ErrNoEmbedder = errors.New("cache: no embedding generator configured")

type generator struct{}

// New returns the no-op embedding generator.
func New() embeddingifaces.EmbeddingGenerator { return generator{} }

func (generator) Name() string { return "none" }

func (generator) Embed(_ context.Context, _ string, _ []string) ([][]float32, error) {
	return nil, ErrNoEmbedder
}
