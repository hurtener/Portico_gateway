package api

import (
	"context"
	"net/http"
	"os"
	"testing"

	llmengine "github.com/hurtener/Portico_gateway/internal/llm/engine"
	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"

	// Side-effect: register the "bifrost" engine driver for this live test.
	_ "github.com/hurtener/Portico_gateway/internal/llm/engine/bifrost"
)

// stubLLMVault is a minimal secrets.Vault that serves one secret.
type stubLLMVault struct{ ref, secret string }

func (v *stubLLMVault) Get(_ context.Context, _, name string) (string, error) {
	if name == v.ref {
		return v.secret, nil
	}
	return "", secrets.ErrNotFound
}
func (v *stubLLMVault) Put(context.Context, string, string, string) error { return nil }
func (v *stubLLMVault) Delete(context.Context, string, string) error      { return nil }
func (v *stubLLMVault) List(context.Context, string) ([]string, error)    { return nil, nil }
func (v *stubLLMVault) RotateKey(context.Context, []byte) error           { return nil }
func (v *stubLLMVault) Close() error                                      { return nil }

// TestLive_ChatCompletions_NorthboundToNIM drives the FULL northbound stack —
// the HTTP handler → alias resolution → the real Bifrost engine → NVIDIA NIM —
// and asserts a real OpenAI-shaped completion. Skipped unless PORTICO_LIVE_LLM_KEY
// is set, so CI never depends on network or credentials.
//
//	PORTICO_LIVE_LLM_KEY=$(cat .devcontainer/secrets/nvidia_api_key) \
//	go test -run TestLive_ChatCompletions_NorthboundToNIM ./internal/server/api/ -v
func TestLive_ChatCompletions_NorthboundToNIM(t *testing.T) {
	key := os.Getenv("PORTICO_LIVE_LLM_KEY")
	if key == "" {
		t.Skip("PORTICO_LIVE_LLM_KEY not set; skipping live northbound test")
	}
	baseURL := os.Getenv("PORTICO_LIVE_LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://integrate.api.nvidia.com"
	}
	model := os.Getenv("PORTICO_LIVE_LLM_MODEL")
	if model == "" {
		model = "meta/llama-3.1-8b-instruct"
	}

	provs := &fakeProvStore{provs: map[string]*storageifaces.LLMProvider{
		"primary": {TenantID: "t1", Name: "primary", Driver: "openai", Enabled: true,
			ConfigJSON: `{"base_url":"` + baseURL + `"}`, CredentialRef: "nim-key"},
	}}
	models := &fakeModelStore{models: map[string]*storageifaces.LLMModel{
		"my-model": {TenantID: "t1", Alias: "my-model", ProviderName: "primary", ProviderModel: model, Enabled: true},
	}}
	eng, err := llmengine.Open("bifrost", nil, engineifaces.Deps{
		Logger:    llmTestLogger(),
		Providers: provs,
		Vault:     &stubLLMVault{ref: "nim-key", secret: key},
	})
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}

	d := Deps{Logger: llmTestLogger(), LLMEngine: eng, LLMModels: models, LLMProviders: provs}
	body := openAIChatRequest{Model: "my-model", Messages: []openAIMessage{{Role: "user", Content: "Reply with exactly: WIRED"}}}
	r := newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke)
	w := runHandler(chatCompletionsHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp openAIChatResponse
	decodeJSON(t, w, &resp)
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		t.Fatalf("empty completion: %+v", resp)
	}
	t.Logf("✅ LIVE northbound /v1/chat/completions → %q | model=%s tokens=%d",
		resp.Choices[0].Message.Content, resp.Model, resp.Usage.TotalTokens)
}
