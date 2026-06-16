package bifrost

import (
	"context"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"

	"github.com/hurtener/Portico_gateway/internal/llm/engine"
	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// --- fakes ---

type fakeProviderStore struct {
	providers []*storageifaces.LLMProvider
	keys      map[string][]*storageifaces.LLMProviderKey // key: tenant|provider
}

func (f *fakeProviderStore) ListProviders(_ context.Context, tenantID string) ([]*storageifaces.LLMProvider, error) {
	out := []*storageifaces.LLMProvider{}
	for _, p := range f.providers {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeProviderStore) GetProvider(_ context.Context, tenantID, name string) (*storageifaces.LLMProvider, error) {
	for _, p := range f.providers {
		if p.TenantID == tenantID && p.Name == name {
			return p, nil
		}
	}
	return nil, storageifaces.ErrLLMProviderNotFound
}

func (f *fakeProviderStore) ListKeys(_ context.Context, tenantID, providerName string) ([]*storageifaces.LLMProviderKey, error) {
	out := []*storageifaces.LLMProviderKey{}
	for _, k := range f.keys[tenantID+"|"+providerName] {
		out = append(out, k)
	}
	return out, nil
}

func (f *fakeProviderStore) CreateProvider(context.Context, *storageifaces.LLMProvider) error {
	return nil
}
func (f *fakeProviderStore) UpdateProvider(context.Context, *storageifaces.LLMProvider) error {
	return nil
}
func (f *fakeProviderStore) DeleteProvider(context.Context, string, string) error        { return nil }
func (f *fakeProviderStore) AddKey(context.Context, *storageifaces.LLMProviderKey) error { return nil }
func (f *fakeProviderStore) DeleteKey(context.Context, string, string, string) error     { return nil }

type fakeVault struct {
	secretsByTenant map[string]map[string]string // tenant -> ref -> value
	getCalls        int
}

func (v *fakeVault) Get(_ context.Context, tenantID, name string) (string, error) {
	v.getCalls++
	if m, ok := v.secretsByTenant[tenantID]; ok {
		if s, ok := m[name]; ok {
			return s, nil
		}
	}
	return "", secrets.ErrNotFound
}
func (v *fakeVault) Put(context.Context, string, string, string) error { return nil }
func (v *fakeVault) Delete(context.Context, string, string) error      { return nil }
func (v *fakeVault) List(context.Context, string) ([]string, error)    { return nil, nil }
func (v *fakeVault) RotateKey(context.Context, []byte) error           { return nil }
func (v *fakeVault) Close() error                                      { return nil }

func testDeps() (ifaces.Deps, *fakeVault) {
	store := &fakeProviderStore{
		providers: []*storageifaces.LLMProvider{
			{TenantID: "tenant-a", Name: "primary", Driver: "openai", Enabled: true, CredentialRef: "openai-default"},
			{TenantID: "tenant-a", Name: "claude", Driver: "anthropic", Enabled: true},
			{TenantID: "tenant-a", Name: "disabled", Driver: "groq", Enabled: false},
			{TenantID: "tenant-b", Name: "primary", Driver: "openai", Enabled: true, CredentialRef: "b-openai"},
		},
		keys: map[string][]*storageifaces.LLMProviderKey{
			"tenant-a|claude": {
				{TenantID: "tenant-a", ProviderName: "claude", KeyID: "k1", CredentialRef: "anthropic-1", Weight: 2.0, ModelAllowlist: `["claude-3-5-sonnet"]`, Enabled: true},
				{TenantID: "tenant-a", ProviderName: "claude", KeyID: "k2", CredentialRef: "anthropic-2", Weight: 1.0, Enabled: false},
			},
		},
	}
	vault := &fakeVault{secretsByTenant: map[string]map[string]string{
		"tenant-a": {"openai-default": "sk-a-openai", "anthropic-1": "sk-a-claude-1"},
		"tenant-b": {"b-openai": "sk-b-openai"},
	}}
	return ifaces.Deps{Providers: store, Vault: vault}, vault
}

// --- account tests ---

func TestAccount_GetConfiguredProviders_TenantScopedAndDeduped(t *testing.T) {
	deps, _ := testDeps()
	acct := &porticoAccount{deps: deps, tenantID: "tenant-a"}
	provs, err := acct.GetConfiguredProviders()
	if err != nil {
		t.Fatalf("GetConfiguredProviders: %v", err)
	}
	got := map[schemas.ModelProvider]bool{}
	for _, p := range provs {
		got[p] = true
	}
	if !got["openai"] || !got["anthropic"] {
		t.Errorf("expected openai+anthropic, got %v", provs)
	}
	if got["groq"] {
		t.Error("disabled provider 'groq' must not be configured")
	}
}

func TestAccount_GetKeysForProvider_FromVaultPerCall(t *testing.T) {
	deps, vault := testDeps()
	acct := &porticoAccount{deps: deps, tenantID: "tenant-a"}

	// anthropic has two key rows; only the enabled one resolves.
	keys, err := acct.GetKeysForProvider(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("GetKeysForProvider: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 enabled key, got %d", len(keys))
	}
	if keys[0].Value.Val != "sk-a-claude-1" {
		t.Errorf("wrong secret resolved: %q", keys[0].Value.Val)
	}
	if keys[0].Weight != 2.0 {
		t.Errorf("weight not mapped: %v", keys[0].Weight)
	}
	if len(keys[0].Models) != 1 || keys[0].Models[0] != "claude-3-5-sonnet" {
		t.Errorf("model allowlist not mapped: %v", keys[0].Models)
	}
	if vault.getCalls == 0 {
		t.Error("expected vault to be read on dispatch (no caching)")
	}

	// openai has no key rows → falls back to the provider's default credential_ref.
	okeys, err := acct.GetKeysForProvider(context.Background(), "openai")
	if err != nil {
		t.Fatalf("GetKeysForProvider openai: %v", err)
	}
	if len(okeys) != 1 || okeys[0].Value.Val != "sk-a-openai" {
		t.Errorf("default credential_ref not resolved: %+v", okeys)
	}
}

func TestAccount_CrossTenantIsolation(t *testing.T) {
	deps, _ := testDeps()
	acctA := &porticoAccount{deps: deps, tenantID: "tenant-a"}
	acctB := &porticoAccount{deps: deps, tenantID: "tenant-b"}

	aKeys, _ := acctA.GetKeysForProvider(context.Background(), "openai")
	bKeys, _ := acctB.GetKeysForProvider(context.Background(), "openai")
	if len(aKeys) != 1 || aKeys[0].Value.Val != "sk-a-openai" {
		t.Errorf("tenant-a openai key wrong: %+v", aKeys)
	}
	if len(bKeys) != 1 || bKeys[0].Value.Val != "sk-b-openai" {
		t.Errorf("tenant-b openai key wrong: %+v", bKeys)
	}
}

func TestAccount_GetConfigForProvider_CustomBaseURL(t *testing.T) {
	store := &fakeProviderStore{providers: []*storageifaces.LLMProvider{
		{TenantID: "t", Name: "deepseek", Driver: "custom_openai", Enabled: true,
			ConfigJSON: `{"base_url":"https://api.deepseek.com/v1","headers":{"X-Org":"acme"}}`},
	}}
	acct := &porticoAccount{deps: ifaces.Deps{Providers: store, Vault: &fakeVault{}}, tenantID: "t"}
	cfg, err := acct.GetConfigForProvider("custom_openai")
	if err != nil {
		t.Fatalf("GetConfigForProvider: %v", err)
	}
	if cfg.NetworkConfig.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("base_url not injected: %q", cfg.NetworkConfig.BaseURL)
	}
	if cfg.NetworkConfig.ExtraHeaders["X-Org"] != "acme" {
		t.Errorf("headers not injected: %v", cfg.NetworkConfig.ExtraHeaders)
	}
}

// --- mapping tests ---

func TestMapping_ChatMessagesAndParams(t *testing.T) {
	temp := 0.7
	max := 256
	req := &ifaces.ChatRequest{
		Messages:    []ifaces.ChatMessage{{Role: "user", Content: "hi"}},
		Temperature: &temp,
		MaxTokens:   &max,
	}
	msgs := toBifrostMessages(req.Messages)
	if len(msgs) != 1 || msgs[0].Role != schemas.ChatMessageRoleUser {
		t.Fatalf("message role mapping wrong: %+v", msgs)
	}
	if msgs[0].Content == nil || msgs[0].Content.ContentStr == nil || *msgs[0].Content.ContentStr != "hi" {
		t.Fatalf("message content mapping wrong: %+v", msgs[0].Content)
	}
	params := toChatParams(req)
	if params == nil || params.Temperature == nil || *params.Temperature != 0.7 {
		t.Fatalf("temperature not mapped: %+v", params)
	}
	if params.MaxCompletionTokens == nil || *params.MaxCompletionTokens != 256 {
		t.Fatalf("max tokens not mapped: %+v", params)
	}
	if toChatParams(&ifaces.ChatRequest{}) != nil {
		t.Error("nil params expected when no temperature/max set")
	}
}

func TestMapping_ChatResponse(t *testing.T) {
	content := "hello world"
	resp := &schemas.BifrostChatResponse{
		ID:    "resp-1",
		Model: "gpt-4o",
		Choices: []schemas.BifrostResponseChoice{{
			ChatNonStreamResponseChoice: &schemas.ChatNonStreamResponseChoice{
				Message: &schemas.ChatMessage{
					Role:    schemas.ChatMessageRoleAssistant,
					Content: &schemas.ChatMessageContent{ContentStr: &content},
				},
			},
		}},
		Usage: &schemas.BifrostLLMUsage{PromptTokens: 11, CompletionTokens: 22, TotalTokens: 33},
	}
	out := fromBifrostChatResponse("openai", resp)
	if out.ID != "resp-1" || out.Model != "gpt-4o" || out.Provider != "openai" {
		t.Errorf("envelope mismatch: %+v", out)
	}
	if out.Message.Content != "hello world" || out.Message.Role != "assistant" {
		t.Errorf("message mismatch: %+v", out.Message)
	}
	if out.Usage.TotalTokens != 33 || out.Usage.PromptTokens != 11 {
		t.Errorf("usage mismatch: %+v", out.Usage)
	}
}

func TestMapping_ModelAllowlist(t *testing.T) {
	// Empty/absent/invalid => nil (Bifrost reads an empty slice as "all models";
	// "*" is a literal, not a wildcard).
	for _, in := range []string{"", "[]", "not json"} {
		if got := modelAllowlist(in); got != nil {
			t.Errorf("modelAllowlist(%q) = %v, want nil (all models)", in, got)
		}
	}
	got := modelAllowlist(`["a","b"]`)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf(`modelAllowlist(["a","b"]) = %v, want [a b]`, got)
	}
}

// --- driver registration / seam test ---

func TestDriver_RegisteredAndBuildsEngine(t *testing.T) {
	deps, _ := testDeps()
	eng, err := engine.Open("bifrost", nil, deps)
	if err != nil {
		t.Fatalf("engine.Open(bifrost): %v", err)
	}
	if eng.Name() != "bifrost" {
		t.Errorf("engine name = %q, want bifrost", eng.Name())
	}
	supported := map[string]bool{}
	for _, d := range eng.ProvidersSupported() {
		supported[d] = true
	}
	if !supported["openai"] || !supported["anthropic"] || !supported["custom_openai"] {
		t.Errorf("ProvidersSupported missing expected drivers: %v", eng.ProvidersSupported())
	}
}
