package api

import (
	"context"
	"net/http"
	"testing"

	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeLLMEngineWithEmbedding extends the fake engine with Embedding support.
type fakeLLMEngineWithEmbedding struct {
	*fakeLLMEngine
	embedResp *engineifaces.EmbeddingResponse
	embedErr  error
}

func (f *fakeLLMEngineWithEmbedding) Embedding(_ context.Context, req *engineifaces.EmbeddingRequest) (*engineifaces.EmbeddingResponse, error) {
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	out := *f.embedResp
	embs := make([][]float64, len(req.Input))
	for i := range embs {
		embs[i] = []float64{0.1, 0.2, 0.3}
	}
	out.Embeddings = embs
	return &out, nil
}

func llmDepsWithEmbedding() (Deps, *fakeLLMEngineWithEmbedding) {
	eng := &fakeLLMEngineWithEmbedding{
		fakeLLMEngine: &fakeLLMEngine{
			resp: &engineifaces.ChatResponse{
				ID:      "resp-1",
				Model:   "gpt-4o",
				Message: engineifaces.ChatMessage{Role: "assistant", Content: "hello from the model"},
				Usage:   engineifaces.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
			},
		},
		embedResp: &engineifaces.EmbeddingResponse{
			Provider: "openai",
			Model:    "text-embedding-3-small",
			Embeddings: [][]float64{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
			},
			Usage: engineifaces.Usage{PromptTokens: 10, CompletionTokens: 0, TotalTokens: 10},
		},
	}
	return Deps{
		Logger:    llmTestLogger(),
		LLMEngine: eng,
		LLMModels: &fakeModelStore{models: map[string]*storageifaces.LLMModel{
			"gpt-4":       {TenantID: "t1", Alias: "gpt-4", ProviderName: "primary", ProviderModel: "gpt-4o", Enabled: true},
			"embed-small": {TenantID: "t1", Alias: "embed-small", ProviderName: "primary", ProviderModel: "text-embedding-3-small", Enabled: true, Capabilities: `["embedding"]`},
		}},
		LLMProviders: &fakeProvStore{provs: map[string]*storageifaces.LLMProvider{
			"primary": {TenantID: "t1", Name: "primary", Driver: "openai", Enabled: true},
		}},
	}, eng
}

func TestEmbeddings_HappyPath(t *testing.T) {
	d, eng := llmDepsWithEmbedding()
	body := map[string]any{
		"model": "embed-small",
		"input": []string{"hello", "world"},
	}
	r := newReq("POST", "/v1/embeddings", body, ScopeLLMInvoke)
	w := runHandler(embeddingsHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp openAIEmbeddingResponse
	decodeJSON(t, w, &resp)
	if resp.Object != "list" || resp.Model != "embed-small" {
		t.Errorf("envelope wrong: %+v", resp)
	}
	if len(resp.Data) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(resp.Data))
	}
	if resp.Data[0].Object != "embedding" || resp.Data[0].Index != 0 {
		t.Errorf("first embedding metadata wrong: %+v", resp.Data[0])
	}
	if len(resp.Data[0].Embedding) != 3 {
		t.Errorf("expected 3-dim embedding, got %d", len(resp.Data[0].Embedding))
	}
	if resp.Usage.TotalTokens != 10 {
		t.Errorf("usage wrong: %+v", resp.Usage)
	}
	// alias resolved to provider driver + upstream model
	if eng.embedResp.Provider != "openai" || eng.embedResp.Model != "text-embedding-3-small" {
		t.Errorf("embedding response metadata wrong: %+v", eng.embedResp)
	}
}

func TestEmbeddings_StringInput(t *testing.T) {
	d, _ := llmDepsWithEmbedding()
	body := map[string]any{
		"model": "embed-small",
		"input": "single string",
	}
	r := newReq("POST", "/v1/embeddings", body, ScopeLLMInvoke)
	w := runHandler(embeddingsHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp openAIEmbeddingResponse
	decodeJSON(t, w, &resp)
	if len(resp.Data) != 1 {
		t.Errorf("expected 1 embedding for string input, got %d", len(resp.Data))
	}
}

func TestEmbeddings_RequiresScope(t *testing.T) {
	d, _ := llmDepsWithEmbedding()
	body := map[string]any{"model": "embed-small", "input": []string{"hi"}}
	r := newReq("POST", "/v1/embeddings", body, "some:other")
	w := runHandler(embeddingsHandler(d), r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestEmbeddings_UnknownModel(t *testing.T) {
	d, _ := llmDepsWithEmbedding()
	body := map[string]any{"model": "nope", "input": []string{"hi"}}
	r := newReq("POST", "/v1/embeddings", body, ScopeLLMInvoke)
	w := runHandler(embeddingsHandler(d), r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEmbeddings_EmptyInput(t *testing.T) {
	d, _ := llmDepsWithEmbedding()
	body := map[string]any{"model": "embed-small", "input": []string{}}
	r := newReq("POST", "/v1/embeddings", body, ScopeLLMInvoke)
	w := runHandler(embeddingsHandler(d), r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestEmbeddings_NotConfigured(t *testing.T) {
	r := newReq("POST", "/v1/embeddings",
		map[string]any{"model": "embed-small", "input": []string{"hi"}}, ScopeLLMInvoke)
	w := runHandler(embeddingsHandler(Deps{Logger: llmTestLogger()}), r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestListModels_HappyPath(t *testing.T) {
	d, _ := llmDepsWithEmbedding()
	r := newReq("GET", "/v1/models", nil, ScopeLLMInvoke)
	w := runHandler(listModelsHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp openAIModelsResponse
	decodeJSON(t, w, &resp)
	if resp.Object != "list" {
		t.Errorf("object != list: %+v", resp)
	}
	if len(resp.Data) != 2 {
		t.Errorf("expected 2 models, got %d: %+v", len(resp.Data), resp.Data)
	}
	for _, m := range resp.Data {
		if m.Object != "model" || m.OwnedBy != "portico" {
			t.Errorf("model entry wrong: %+v", m)
		}
	}
}

func TestListModels_OnlyEnabled(t *testing.T) {
	d, _ := llmDepsWithEmbedding()
	// Add a disabled model
	d.LLMModels.(*fakeModelStore).models["disabled-model"] = &storageifaces.LLMModel{
		TenantID: "t1", Alias: "disabled-model", ProviderName: "primary", ProviderModel: "gpt-3.5", Enabled: false,
	}
	r := newReq("GET", "/v1/models", nil, ScopeLLMInvoke)
	w := runHandler(listModelsHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp openAIModelsResponse
	decodeJSON(t, w, &resp)
	if len(resp.Data) != 2 {
		t.Errorf("expected 2 enabled models, got %d: %+v", len(resp.Data), resp.Data)
	}
}

func TestListModels_NotConfigured(t *testing.T) {
	r := newReq("GET", "/v1/models", nil, ScopeLLMInvoke)
	w := runHandler(listModelsHandler(Deps{Logger: llmTestLogger()}), r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
