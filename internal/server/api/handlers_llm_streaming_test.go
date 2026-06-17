package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeStreamEngine is a fake engine that supports streaming.
type fakeStreamEngine struct {
	chunks []engineifaces.ChatChunk
	err    error
	gotReq *engineifaces.ChatRequest
}

func (f *fakeStreamEngine) Name() string { return "fake-stream" }

func (f *fakeStreamEngine) ChatCompletion(_ context.Context, req *engineifaces.ChatRequest) (*engineifaces.ChatResponse, error) {
	f.gotReq = req
	return &engineifaces.ChatResponse{
		ID:      "resp-1",
		Model:   "gpt-4o",
		Message: engineifaces.ChatMessage{Role: "assistant", Content: "hello"},
		Usage:   engineifaces.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
	}, nil
}

func (f *fakeStreamEngine) ChatCompletionStream(ctx context.Context, req *engineifaces.ChatRequest) (<-chan engineifaces.ChatChunk, error) {
	f.gotReq = req
	ch := make(chan engineifaces.ChatChunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, f.err
}

func (f *fakeStreamEngine) Embedding(context.Context, *engineifaces.EmbeddingRequest) (*engineifaces.EmbeddingResponse, error) {
	return nil, nil
}

func (f *fakeStreamEngine) ProvidersSupported() []string { return []string{"openai"} }

func (f *fakeStreamEngine) Health(context.Context) ([]engineifaces.ProviderHealth, error) {
	return nil, nil
}

func streamDeps(chunks []engineifaces.ChatChunk) (Deps, *fakeStreamEngine) {
	eng := &fakeStreamEngine{chunks: chunks}
	return Deps{
		Logger:       llmTestLogger(),
		LLMEngine:    eng,
		LLMModels:    &fakeModelStore{models: map[string]*storageifaces.LLMModel{"gpt-4": {TenantID: "t1", Alias: "gpt-4", ProviderName: "primary", ProviderModel: "gpt-4o", Enabled: true}}},
		LLMProviders: &fakeProvStore{provs: map[string]*storageifaces.LLMProvider{"primary": {TenantID: "t1", Name: "primary", Driver: "openai", Enabled: true}}},
	}, eng
}

// --- tests ---

func TestStreamChatCompletion_HappyPath(t *testing.T) {
	chunks := []engineifaces.ChatChunk{
		{Delta: "Hello", Done: false},
		{Delta: " world", Done: false},
		{Delta: "", Done: true},
	}
	d, eng := streamDeps(chunks)

	body := openAIChatRequest{Model: "gpt-4", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", w.Header().Get("Content-Type"))
	}

	respBody := w.Body.String()
	if !strings.Contains(respBody, "data:") {
		t.Errorf("response missing SSE data events: %s", respBody)
	}

	// Check for the two delta chunks
	if !strings.Contains(respBody, `"content":"Hello"`) {
		t.Errorf("missing first delta chunk: %s", respBody)
	}
	if !strings.Contains(respBody, `"content":" world"`) {
		t.Errorf("missing second delta chunk: %s", respBody)
	}

	// Check for final chunk with finish_reason: "stop"
	if !strings.Contains(respBody, `"finish_reason":"stop"`) {
		t.Errorf("missing finish_reason stop: %s", respBody)
	}

	// Check for terminal [DONE]
	if !strings.Contains(respBody, "data: [DONE]") {
		t.Errorf("missing terminal [DONE]: %s", respBody)
	}

	// Verify engine was called with correct params
	if eng.gotReq == nil {
		t.Fatal("engine ChatCompletionStream not called")
	}
	if eng.gotReq.Provider != "openai" || eng.gotReq.ProviderModel != "gpt-4o" {
		t.Errorf("alias resolution wrong: %+v", eng.gotReq)
	}
	if eng.gotReq.TenantID != "t1" {
		t.Errorf("tenant not propagated: %q", eng.gotReq.TenantID)
	}
	if !eng.gotReq.Stream {
		t.Errorf("Stream flag not set on engine request")
	}
}

func TestStreamChatCompletion_EngineError(t *testing.T) {
	eng := &fakeStreamEngine{err: engineifaces.ErrUpstreamUnavailable}
	eng.chunks = nil
	d := Deps{
		Logger:       llmTestLogger(),
		LLMEngine:    eng,
		LLMModels:    &fakeModelStore{models: map[string]*storageifaces.LLMModel{"gpt-4": {TenantID: "t1", Alias: "gpt-4", ProviderName: "primary", ProviderModel: "gpt-4o", Enabled: true}}},
		LLMProviders: &fakeProvStore{provs: map[string]*storageifaces.LLMProvider{"primary": {TenantID: "t1", Name: "primary", Driver: "openai", Enabled: true}}},
	}

	body := openAIChatRequest{Model: "gpt-4", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)

	if w.Code != http.StatusOK {
		t.Fatalf("streaming handler returns 200 even on engine error (SSE error event), got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", w.Header().Get("Content-Type"))
	}

	respBody := w.Body.String()
	if !strings.Contains(respBody, "stream_error") {
		t.Errorf("missing SSE error event: %s", respBody)
	}
}

func TestStreamChatCompletion_ChunkError(t *testing.T) {
	chunks := []engineifaces.ChatChunk{
		{Delta: "Hello", Done: false},
		{Delta: "", Done: false, Err: engineifaces.ErrUpstreamUnavailable},
	}
	d, _ := streamDeps(chunks)

	body := openAIChatRequest{Model: "gpt-4", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	respBody := w.Body.String()
	if !strings.Contains(respBody, "stream_error") {
		t.Errorf("missing SSE error event on mid-stream error: %s", respBody)
	}
}

func TestStreamChatCompletion_RequiresScope(t *testing.T) {
	d, _ := streamDeps([]engineifaces.ChatChunk{{Delta: "x", Done: true}})
	body := openAIChatRequest{Model: "gpt-4", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, "some:other")
	w := runHandler(chatCompletionsHandler(d), r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestStreamChatCompletion_UnknownModel(t *testing.T) {
	d, _ := streamDeps([]engineifaces.ChatChunk{{Delta: "x", Done: true}})
	body := openAIChatRequest{Model: "nope", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStreamChatCompletion_NotConfigured(t *testing.T) {
	r := newReq("POST", "/v1/chat/completions", openAIChatRequest{Model: "gpt-4", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(Deps{Logger: llmTestLogger()}), r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestStreamChatCompletion_DisabledModel(t *testing.T) {
	d, _ := streamDeps([]engineifaces.ChatChunk{{Delta: "x", Done: true}})
	d.LLMModels = &fakeModelStore{models: map[string]*storageifaces.LLMModel{"gpt-4": {TenantID: "t1", Alias: "gpt-4", ProviderName: "primary", ProviderModel: "gpt-4o", Enabled: false}}}
	body := openAIChatRequest{Model: "gpt-4", Stream: true, Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled model, got %d", w.Code)
	}
}
