package bifrost

import (
	"context"
	"os"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/llm/engine"
	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// TestLive_OpenAICompatible_ChatCompletion exercises the FULL dispatch chain
// (account → vault → Bifrost OpenAI code path → real upstream → mapped response)
// against an OpenAI-compatible endpoint. It is skipped unless a real key is
// provided, so CI never depends on network or credentials.
//
// Run locally against NVIDIA NIM:
//
//	PORTICO_LIVE_LLM_KEY=$(cat .devcontainer/secrets/nvidia_api_key) \
//	PORTICO_LIVE_LLM_BASE_URL=https://integrate.api.nvidia.com \
//	PORTICO_LIVE_LLM_MODEL=meta/llama-3.1-8b-instruct \
//	go test -run TestLive_OpenAICompatible_ChatCompletion ./internal/llm/engine/bifrost/ -v
func TestLive_OpenAICompatible_ChatCompletion(t *testing.T) {
	key := os.Getenv("PORTICO_LIVE_LLM_KEY")
	if key == "" {
		t.Skip("PORTICO_LIVE_LLM_KEY not set; skipping live integration test")
	}
	baseURL := os.Getenv("PORTICO_LIVE_LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://integrate.api.nvidia.com"
	}
	model := os.Getenv("PORTICO_LIVE_LLM_MODEL")
	if model == "" {
		model = "meta/llama-3.1-8b-instruct"
	}

	store := &fakeProviderStore{providers: []*storageifaces.LLMProvider{
		{TenantID: "live", Name: "nim", Driver: "openai", Enabled: true,
			ConfigJSON: `{"base_url":"` + baseURL + `"}`, CredentialRef: "nim-key"},
	}}
	vault := &fakeVault{secretsByTenant: map[string]map[string]string{"live": {"nim-key": key}}}
	eng, err := engine.Open("bifrost", nil, ifaces.Deps{Providers: store, Vault: vault})
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}

	resp, err := eng.ChatCompletion(context.Background(), &ifaces.ChatRequest{
		TenantID:      "live",
		Provider:      "openai",
		ProviderModel: model,
		Messages: []ifaces.ChatMessage{
			{Role: "user", Content: "Reply with exactly the single word: WIRED"},
		},
	})
	if err != nil {
		t.Fatalf("live ChatCompletion failed: %v", err)
	}
	if resp.Message.Content == "" {
		t.Fatalf("live ChatCompletion returned empty content: %+v", resp)
	}
	t.Logf("✅ LIVE NIM response: %q | role=%s model=%s tokens(p/c/t)=%d/%d/%d",
		resp.Message.Content, resp.Message.Role, resp.Model,
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
}
