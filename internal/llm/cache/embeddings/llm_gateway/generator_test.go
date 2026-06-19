package llmgateway

import (
	"context"
	"errors"
	"testing"

	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// --- minimal fakes (only the methods the generator calls) ---

type fakeEngine struct {
	resp *engineifaces.EmbeddingResponse
	err  error
	got  *engineifaces.EmbeddingRequest
}

func (f *fakeEngine) Name() string { return "fake" }
func (f *fakeEngine) ChatCompletion(context.Context, *engineifaces.ChatRequest) (*engineifaces.ChatResponse, error) {
	return nil, nil
}
func (f *fakeEngine) ChatCompletionStream(context.Context, *engineifaces.ChatRequest) (<-chan engineifaces.ChatChunk, error) {
	return nil, nil
}
func (f *fakeEngine) Embedding(_ context.Context, req *engineifaces.EmbeddingRequest) (*engineifaces.EmbeddingResponse, error) {
	f.got = req
	return f.resp, f.err
}
func (f *fakeEngine) ProvidersSupported() []string { return nil }
func (f *fakeEngine) Health(context.Context) ([]engineifaces.ProviderHealth, error) {
	return nil, nil
}

type fakeModels struct{ m *storageifaces.LLMModel }

func (f *fakeModels) GetModel(_ context.Context, _, _ string) (*storageifaces.LLMModel, error) {
	if f.m == nil {
		return nil, storageifaces.ErrLLMModelNotFound
	}
	return f.m, nil
}
func (f *fakeModels) CreateModel(context.Context, *storageifaces.LLMModel) error { return nil }
func (f *fakeModels) ListModels(context.Context, string) ([]*storageifaces.LLMModel, error) {
	return nil, nil
}
func (f *fakeModels) UpdateModel(context.Context, *storageifaces.LLMModel) error { return nil }
func (f *fakeModels) DeleteModel(context.Context, string, string) error          { return nil }

type fakeProviders struct{ p *storageifaces.LLMProvider }

func (f *fakeProviders) GetProvider(_ context.Context, _, _ string) (*storageifaces.LLMProvider, error) {
	if f.p == nil {
		return nil, storageifaces.ErrLLMProviderNotFound
	}
	return f.p, nil
}
func (f *fakeProviders) ListProviders(context.Context, string) ([]*storageifaces.LLMProvider, error) {
	return nil, nil
}
func (f *fakeProviders) CreateProvider(context.Context, *storageifaces.LLMProvider) error { return nil }
func (f *fakeProviders) UpdateProvider(context.Context, *storageifaces.LLMProvider) error { return nil }
func (f *fakeProviders) DeleteProvider(context.Context, string, string) error             { return nil }
func (f *fakeProviders) AddKey(context.Context, *storageifaces.LLMProviderKey) error      { return nil }
func (f *fakeProviders) ListKeys(context.Context, string, string) ([]*storageifaces.LLMProviderKey, error) {
	return nil, nil
}
func (f *fakeProviders) DeleteKey(context.Context, string, string, string) error { return nil }

func TestEmbed_NotConfigured(t *testing.T) {
	g := New(Deps{}) // nil engine/stores/alias
	if _, err := g.Embed(context.Background(), "t", []string{"hi"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	if g.Name() != "llm_gateway" {
		t.Fatalf("name: %q", g.Name())
	}
}

func TestEmbed_RoutesThroughGatewayAndNarrowsToFloat32(t *testing.T) {
	eng := &fakeEngine{resp: &engineifaces.EmbeddingResponse{
		Embeddings: [][]float64{{0.5, -0.25}, {1.0, 0.0}},
	}}
	g := New(Deps{
		Engine:    eng,
		Models:    &fakeModels{m: &storageifaces.LLMModel{ProviderName: "p", ProviderModel: "text-embed-3", Enabled: true}},
		Providers: &fakeProviders{p: &storageifaces.LLMProvider{Driver: "openai"}},
		Alias:     "cache-embed",
	})
	out, err := g.Embed(context.Background(), "tenant-a", []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(out) != 2 || len(out[0]) != 2 || out[0][0] != float32(0.5) || out[0][1] != float32(-0.25) {
		t.Fatalf("vectors not narrowed correctly: %+v", out)
	}
	// The request was routed with the resolved provider driver + upstream model.
	if eng.got.Provider != "openai" || eng.got.ProviderModel != "text-embed-3" || eng.got.TenantID != "tenant-a" {
		t.Fatalf("engine request wrong: %+v", eng.got)
	}
}

func TestEmbed_EmptyInput(t *testing.T) {
	g := New(Deps{
		Engine:    &fakeEngine{},
		Models:    &fakeModels{m: &storageifaces.LLMModel{}},
		Providers: &fakeProviders{p: &storageifaces.LLMProvider{}},
		Alias:     "x",
	})
	if v, err := g.Embed(context.Background(), "t", nil); err != nil || v != nil {
		t.Fatalf("empty input → (nil,nil); got (%v,%v)", v, err)
	}
}
