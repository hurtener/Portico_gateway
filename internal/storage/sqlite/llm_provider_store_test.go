package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestLLMProviderStore_CRUD(t *testing.T) {
	db := open(t)
	store := db.LLMProviders()
	ctx := context.Background()

	// Create provider
	p := &ifaces.LLMProvider{
		TenantID:      "tenant-a",
		Name:          "openai",
		Driver:        "openai",
		ConfigJSON:    `{"model":"gpt-4"}`,
		CredentialRef: "vault:openai-key",
		Enabled:       true,
	}
	if err := store.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.CreatedAt == "" || p.UpdatedAt == "" {
		t.Fatal("created_at/updated_at not set")
	}

	// Get provider
	got, err := store.GetProvider(ctx, "tenant-a", "openai")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "openai" || got.Driver != "openai" || got.ConfigJSON != `{"model":"gpt-4"}` || got.CredentialRef != "vault:openai-key" || !got.Enabled {
		t.Errorf("get mismatch: %+v", got)
	}

	// List providers
	all, err := store.ListProviders(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].Name != "openai" {
		t.Errorf("list mismatch: %+v", all)
	}

	// Update provider
	p.ConfigJSON = `{"model":"gpt-4o"}`
	p.Enabled = false
	if err := store.UpdateProvider(ctx, p); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = store.GetProvider(ctx, "tenant-a", "openai")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.ConfigJSON != `{"model":"gpt-4o"}` || got.Enabled {
		t.Errorf("update not persisted: %+v", got)
	}

	// Add key
	k := &ifaces.LLMProviderKey{
		TenantID:       "tenant-a",
		ProviderName:   "openai",
		KeyID:          "01HX0000000000000000000001",
		CredentialRef:  "vault:openai-key-1",
		Weight:         1.5,
		ModelAllowlist: `["gpt-4", "gpt-4o"]`,
		Enabled:        true,
	}
	if err := store.AddKey(ctx, k); err != nil {
		t.Fatalf("add key: %v", err)
	}
	if k.CreatedAt == "" {
		t.Fatal("key created_at not set")
	}

	// List keys
	keys, err := store.ListKeys(ctx, "tenant-a", "openai")
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 || keys[0].KeyID != k.KeyID || keys[0].Weight != 1.5 || keys[0].ModelAllowlist != `["gpt-4", "gpt-4o"]` || !keys[0].Enabled {
		t.Errorf("list keys mismatch: %+v", keys)
	}

	// Delete key
	if err := store.DeleteKey(ctx, "tenant-a", "openai", k.KeyID); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	keys, err = store.ListKeys(ctx, "tenant-a", "openai")
	if err != nil {
		t.Fatalf("list keys after delete: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("keys not deleted: %+v", keys)
	}

	// Delete provider (cascades keys)
	if err := store.DeleteProvider(ctx, "tenant-a", "openai"); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	_, err = store.GetProvider(ctx, "tenant-a", "openai")
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("expected ErrLLMProviderNotFound after delete, got: %v", err)
	}
	keys, _ = store.ListKeys(ctx, "tenant-a", "openai")
	if len(keys) != 0 {
		t.Errorf("keys not cascade-deleted: %+v", keys)
	}
}

func TestLLMProviderStore_CrossTenantIsolation(t *testing.T) {
	db := open(t)
	store := db.LLMProviders()
	ctx := context.Background()

	// Tenant A creates a provider and key
	pA := &ifaces.LLMProvider{
		TenantID:      "tenant-a",
		Name:          "anthropic",
		Driver:        "anthropic",
		ConfigJSON:    `{"region":"us-east-1"}`,
		CredentialRef: "vault:anthropic-key",
		Enabled:       true,
	}
	if err := store.CreateProvider(ctx, pA); err != nil {
		t.Fatalf("create A: %v", err)
	}
	kA := &ifaces.LLMProviderKey{
		TenantID:       "tenant-a",
		ProviderName:   "anthropic",
		KeyID:          "01HX000000000000000000000A",
		CredentialRef:  "vault:anthropic-key-1",
		Weight:         2.0,
		ModelAllowlist: `[]`,
		Enabled:        true,
	}
	if err := store.AddKey(ctx, kA); err != nil {
		t.Fatalf("add key A: %v", err)
	}

	// Tenant B creates a different provider
	pB := &ifaces.LLMProvider{
		TenantID:      "tenant-b",
		Name:          "openai",
		Driver:        "openai",
		ConfigJSON:    `{"org_id":"org-123"}`,
		CredentialRef: "vault:openai-key-b",
		Enabled:       true,
	}
	if err := store.CreateProvider(ctx, pB); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Tenant A should see only their provider
	aProviders, err := store.ListProviders(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(aProviders) != 1 || aProviders[0].Name != "anthropic" {
		t.Errorf("tenant-a sees wrong providers: %+v", aProviders)
	}
	aKeys, err := store.ListKeys(ctx, "tenant-a", "anthropic")
	if err != nil {
		t.Fatalf("list keys A: %v", err)
	}
	if len(aKeys) != 1 || aKeys[0].KeyID != kA.KeyID {
		t.Errorf("tenant-a sees wrong keys: %+v", aKeys)
	}

	// Tenant B should see only their provider
	bProviders, err := store.ListProviders(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(bProviders) != 1 || bProviders[0].Name != "openai" {
		t.Errorf("tenant-b sees wrong providers: %+v", bProviders)
	}

	// Tenant A cannot get Tenant B's provider
	_, err = store.GetProvider(ctx, "tenant-a", "openai")
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("tenant-a get tenant-b provider: expected ErrLLMProviderNotFound, got: %v", err)
	}

	// Tenant B cannot get Tenant A's provider
	_, err = store.GetProvider(ctx, "tenant-b", "anthropic")
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("tenant-b get tenant-a provider: expected ErrLLMProviderNotFound, got: %v", err)
	}

	// Tenant A cannot list Tenant B's keys
	_, err = store.ListKeys(ctx, "tenant-a", "openai")
	if err != nil {
		t.Fatalf("tenant-a list tenant-b keys: %v", err)
	}

	// Tenant B cannot list Tenant A's keys
	_, err = store.ListKeys(ctx, "tenant-b", "anthropic")
	if err != nil {
		t.Fatalf("tenant-b list tenant-a keys: %v", err)
	}

	// Update/delete with wrong tenant returns not-found
	err = store.UpdateProvider(ctx, &ifaces.LLMProvider{
		TenantID: "tenant-a", Name: "openai", Driver: "openai", ConfigJSON: "{}", Enabled: true,
	})
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("update wrong tenant: expected ErrLLMProviderNotFound, got: %v", err)
	}
	err = store.DeleteProvider(ctx, "tenant-a", "openai")
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("delete wrong tenant: expected ErrLLMProviderNotFound, got: %v", err)
	}
	err = store.DeleteKey(ctx, "tenant-a", "anthropic", "nonexistent")
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("delete key wrong tenant: expected ErrLLMProviderNotFound, got: %v", err)
	}
}

func TestLLMProviderStore_NotFound(t *testing.T) {
	db := open(t)
	store := db.LLMProviders()
	ctx := context.Background()

	_, err := store.GetProvider(ctx, "ghost", "nonexistent")
	if !errors.Is(err, ifaces.ErrLLMProviderNotFound) {
		t.Fatalf("expected ErrLLMProviderNotFound, got: %v", err)
	}

	// List on empty tenant returns empty slice, not error
	list, err := store.ListProviders(ctx, "empty-tenant")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestLLMProviderStore_Defaults(t *testing.T) {
	db := open(t)
	store := db.LLMProviders()
	ctx := context.Background()

	// ConfigJSON defaults to {}
	p := &ifaces.LLMProvider{
		TenantID: "tenant-x",
		Name:     "custom",
		Driver:   "custom_openai",
		Enabled:  true,
		// ConfigJSON, CredentialRef omitted
	}
	if err := store.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create with defaults: %v", err)
	}
	got, err := store.GetProvider(ctx, "tenant-x", "custom")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ConfigJSON != "{}" {
		t.Errorf("ConfigJSON default: got %q, want {}", got.ConfigJSON)
	}
	if got.CredentialRef != "" {
		t.Errorf("CredentialRef default: got %q, want empty", got.CredentialRef)
	}

	// Key defaults: ModelAllowlist=[], Weight=1.0
	k := &ifaces.LLMProviderKey{
		TenantID:      "tenant-x",
		ProviderName:  "custom",
		KeyID:         "01HX000000000000000000000X",
		CredentialRef: "vault:key",
		Enabled:       true,
		// Weight, ModelAllowlist omitted
	}
	if err := store.AddKey(ctx, k); err != nil {
		t.Fatalf("add key with defaults: %v", err)
	}
	keys, err := store.ListKeys(ctx, "tenant-x", "custom")
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Weight != 1.0 {
		t.Errorf("Weight default: got %f, want 1.0", keys[0].Weight)
	}
	if keys[0].ModelAllowlist != "[]" {
		t.Errorf("ModelAllowlist default: got %q, want []", keys[0].ModelAllowlist)
	}
}
