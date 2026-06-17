package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// --- fakes ---

type fakeLLMEngine struct {
	gotReq *engineifaces.ChatRequest
	resp   *engineifaces.ChatResponse
	err    error
}

func (f *fakeLLMEngine) Name() string { return "fake" }
func (f *fakeLLMEngine) ChatCompletion(_ context.Context, req *engineifaces.ChatRequest) (*engineifaces.ChatResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}
func (f *fakeLLMEngine) ChatCompletionStream(context.Context, *engineifaces.ChatRequest) (<-chan engineifaces.ChatChunk, error) {
	return nil, nil
}
func (f *fakeLLMEngine) Embedding(context.Context, *engineifaces.EmbeddingRequest) (*engineifaces.EmbeddingResponse, error) {
	return nil, nil
}
func (f *fakeLLMEngine) ProvidersSupported() []string { return []string{"openai"} }
func (f *fakeLLMEngine) Health(context.Context) ([]engineifaces.ProviderHealth, error) {
	return nil, nil
}

type fakeModelStore struct {
	models map[string]*storageifaces.LLMModel // alias -> model (tenant t1)
}

func (f *fakeModelStore) GetModel(_ context.Context, tenantID, alias string) (*storageifaces.LLMModel, error) {
	if m, ok := f.models[alias]; ok && tenantID == "t1" {
		return m, nil
	}
	return nil, storageifaces.ErrLLMModelNotFound
}
func (f *fakeModelStore) CreateModel(context.Context, *storageifaces.LLMModel) error { return nil }
func (f *fakeModelStore) ListModels(_ context.Context, tenantID string) ([]*storageifaces.LLMModel, error) {
	out := []*storageifaces.LLMModel{}
	for _, m := range f.models {
		if m.TenantID == tenantID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (f *fakeModelStore) UpdateModel(context.Context, *storageifaces.LLMModel) error { return nil }
func (f *fakeModelStore) DeleteModel(context.Context, string, string) error          { return nil }

type fakeProvStore struct {
	provs map[string]*storageifaces.LLMProvider // name -> provider (tenant t1)
}

func (f *fakeProvStore) GetProvider(_ context.Context, tenantID, name string) (*storageifaces.LLMProvider, error) {
	if p, ok := f.provs[name]; ok && tenantID == "t1" {
		return p, nil
	}
	return nil, storageifaces.ErrLLMProviderNotFound
}
func (f *fakeProvStore) ListProviders(_ context.Context, tenantID string) ([]*storageifaces.LLMProvider, error) {
	out := []*storageifaces.LLMProvider{}
	for _, p := range f.provs {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeProvStore) CreateProvider(context.Context, *storageifaces.LLMProvider) error { return nil }
func (f *fakeProvStore) UpdateProvider(context.Context, *storageifaces.LLMProvider) error { return nil }
func (f *fakeProvStore) DeleteProvider(context.Context, string, string) error             { return nil }
func (f *fakeProvStore) AddKey(context.Context, *storageifaces.LLMProviderKey) error      { return nil }
func (f *fakeProvStore) ListKeys(context.Context, string, string) ([]*storageifaces.LLMProviderKey, error) {
	return nil, nil
}
func (f *fakeProvStore) DeleteKey(context.Context, string, string, string) error { return nil }

func llmDeps() (Deps, *fakeLLMEngine) {
	eng := &fakeLLMEngine{resp: &engineifaces.ChatResponse{
		ID: "resp-1", Model: "gpt-4o",
		Message: engineifaces.ChatMessage{Role: "assistant", Content: "hello from the model"},
		Usage:   engineifaces.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}}
	return Deps{
		Logger:    llmTestLogger(),
		LLMEngine: eng,
		LLMModels: &fakeModelStore{models: map[string]*storageifaces.LLMModel{
			"gpt-4": {TenantID: "t1", Alias: "gpt-4", ProviderName: "primary", ProviderModel: "gpt-4o", Enabled: true},
		}},
		LLMProviders: &fakeProvStore{provs: map[string]*storageifaces.LLMProvider{
			"primary": {TenantID: "t1", Name: "primary", Driver: "openai", Enabled: true},
		}},
	}, eng
}

// --- tests ---

func TestChatCompletions_HappyPath(t *testing.T) {
	d, eng := llmDeps()
	body := openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp openAIChatResponse
	decodeJSON(t, w, &resp)
	if resp.Object != "chat.completion" || resp.Model != "gpt-4" {
		t.Errorf("envelope wrong: %+v", resp)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello from the model" {
		t.Errorf("choices wrong: %+v", resp.Choices)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("usage wrong: %+v", resp.Usage)
	}
	// alias resolved to provider driver + upstream model.
	if eng.gotReq.Provider != "openai" || eng.gotReq.ProviderModel != "gpt-4o" {
		t.Errorf("alias resolution wrong: %+v", eng.gotReq)
	}
	if eng.gotReq.TenantID != "t1" {
		t.Errorf("tenant not propagated: %q", eng.gotReq.TenantID)
	}
}

func TestChatCompletions_RequiresScope(t *testing.T) {
	d, _ := llmDeps()
	body := openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	// no llm:invoke scope (use an unrelated scope)
	r := newReq("POST", "/v1/chat/completions", body, "some:other")
	w := runHandler(chatCompletionsHandler(d), r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestChatCompletions_UnknownModel(t *testing.T) {
	d, _ := llmDeps()
	body := openAIChatRequest{Model: "nope", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_NotConfigured(t *testing.T) {
	r := newReq("POST", "/v1/chat/completions",
		openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(Deps{Logger: llmTestLogger()}), r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func llmTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
