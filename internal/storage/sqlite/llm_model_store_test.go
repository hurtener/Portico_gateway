package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestLLMModelStore_CRUD(t *testing.T) {
	db := open(t)
	modelStore := db.LLMModels()
	providerStore := db.LLMProviders()
	ctx := context.Background()

	// Create provider first (FK requirement)
	p := &ifaces.LLMProvider{
		TenantID:   "tenant-a",
		Name:       "openai",
		Driver:     "openai",
		ConfigJSON: `{"endpoint": "https://api.openai.com/v1"}`,
		Enabled:    true,
	}
	if err := providerStore.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Create model
	m := &ifaces.LLMModel{
		TenantID:          "tenant-a",
		Alias:             "gpt-4",
		ProviderName:      "openai",
		ProviderModel:     "gpt-4o",
		DefaultParamsJSON: `{"temperature": 0.7, "max_tokens": 2000}`,
		Capabilities:      `["chat", "tool_use"]`,
		Enabled:           true,
	}
	if err := modelStore.CreateModel(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.CreatedAt == "" || m.UpdatedAt == "" {
		t.Fatal("created_at/updated_at not set")
	}

	// Get model (alias resolver)
	got, err := modelStore.GetModel(ctx, "tenant-a", "gpt-4")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Alias != "gpt-4" || got.ProviderName != "openai" || got.ProviderModel != "gpt-4o" || got.DefaultParamsJSON != `{"temperature": 0.7, "max_tokens": 2000}` || got.Capabilities != `["chat", "tool_use"]` || !got.Enabled {
		t.Errorf("get mismatch: %+v", got)
	}

	// List models
	all, err := modelStore.ListModels(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].Alias != "gpt-4" {
		t.Errorf("list mismatch: %+v", all)
	}

	// Update model
	m.DefaultParamsJSON = `{"temperature": 0.5, "max_tokens": 1000}`
	m.Capabilities = `["chat", "completion", "tool_use"]`
	m.Enabled = false
	if err := modelStore.UpdateModel(ctx, m); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = modelStore.GetModel(ctx, "tenant-a", "gpt-4")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.DefaultParamsJSON != `{"temperature": 0.5, "max_tokens": 1000}` || got.Capabilities != `["chat", "completion", "tool_use"]` || got.Enabled {
		t.Errorf("update not persisted: %+v", got)
	}

	// Delete model
	if err := modelStore.DeleteModel(ctx, "tenant-a", "gpt-4"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = modelStore.GetModel(ctx, "tenant-a", "gpt-4")
	if !errors.Is(err, ifaces.ErrLLMModelNotFound) {
		t.Fatalf("expected ErrLLMModelNotFound after delete, got: %v", err)
	}
}

func TestLLMModelStore_CrossTenantIsolation(t *testing.T) {
	db := open(t)
	modelStore := db.LLMModels()
	providerStore := db.LLMProviders()
	ctx := context.Background()

	// Tenant A creates provider and model
	pA := &ifaces.LLMProvider{
		TenantID:   "tenant-a",
		Name:       "openai",
		Driver:     "openai",
		ConfigJSON: `{"endpoint": "https://api.openai.com/v1"}`,
		Enabled:    true,
	}
	if err := providerStore.CreateProvider(ctx, pA); err != nil {
		t.Fatalf("create provider A: %v", err)
	}
	mA := &ifaces.LLMModel{
		TenantID:      "tenant-a",
		Alias:         "gpt-4",
		ProviderName:  "openai",
		ProviderModel: "gpt-4o",
		Enabled:       true,
	}
	if err := modelStore.CreateModel(ctx, mA); err != nil {
		t.Fatalf("create model A: %v", err)
	}

	// Tenant B creates provider and model with SAME alias
	pB := &ifaces.LLMProvider{
		TenantID:   "tenant-b",
		Name:       "azure",
		Driver:     "azure",
		ConfigJSON: `{"endpoint": "https://my-azure.openai.azure.com"}`,
		Enabled:    true,
	}
	if err := providerStore.CreateProvider(ctx, pB); err != nil {
		t.Fatalf("create provider B: %v", err)
	}
	mB := &ifaces.LLMModel{
		TenantID:      "tenant-b",
		Alias:         "gpt-4",
		ProviderName:  "azure",
		ProviderModel: "gpt-4-deployment-1",
		Enabled:       true,
	}
	if err := modelStore.CreateModel(ctx, mB); err != nil {
		t.Fatalf("create model B: %v", err)
	}

	// Tenant A should see only their model
	aModels, err := modelStore.ListModels(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(aModels) != 1 {
		t.Errorf("tenant-a sees wrong model count: %+v", aModels)
	} else if a := aModels[0]; a.ProviderName != "openai" || a.ProviderModel != "gpt-4o" {
		t.Errorf("tenant-a sees wrong model: %+v", a)
	}

	// Tenant B should see only their model
	bModels, err := modelStore.ListModels(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(bModels) != 1 {
		t.Errorf("tenant-b sees wrong model count: %+v", bModels)
	} else if b := bModels[0]; b.ProviderName != "azure" || b.ProviderModel != "gpt-4-deployment-1" {
		t.Errorf("tenant-b sees wrong model: %+v", b)
	}

	// Tenant A's GetModel with their own alias returns THEIR model (not tenant-b's)
	gotA, err := modelStore.GetModel(ctx, "tenant-a", "gpt-4")
	if err != nil {
		t.Fatalf("get A: %v", err)
	}
	if gotA.ProviderName != "openai" || gotA.ProviderModel != "gpt-4o" {
		t.Errorf("tenant-a got wrong model: %+v", gotA)
	}

	// Tenant B's GetModel with their own alias returns THEIR model (not tenant-a's)
	gotB, err := modelStore.GetModel(ctx, "tenant-b", "gpt-4")
	if err != nil {
		t.Fatalf("get B: %v", err)
	}
	if gotB.ProviderName != "azure" || gotB.ProviderModel != "gpt-4-deployment-1" {
		t.Errorf("tenant-b got wrong model: %+v", gotB)
	}

	// Tenant B creates a second model with a unique alias
	mB2 := &ifaces.LLMModel{
		TenantID:      "tenant-b",
		Alias:         "claude-3",
		ProviderName:  "azure",
		ProviderModel: "claude-3-sonnet",
		Enabled:       true,
	}
	if err := modelStore.CreateModel(ctx, mB2); err != nil {
		t.Fatalf("create model B2: %v", err)
	}

	// Tenant A cannot UPDATE Tenant B's unique model (using tenant-a's context)
	// This tests that UpdateModel filters by tenant_id - tenant-a has no "claude-3" model
	err = modelStore.UpdateModel(ctx, &ifaces.LLMModel{
		TenantID: "tenant-a", Alias: "claude-3", ProviderName: "openai", ProviderModel: "gpt-4o", Enabled: true,
	})
	if !errors.Is(err, ifaces.ErrLLMModelNotFound) {
		t.Fatalf("update wrong tenant: expected ErrLLMModelNotFound, got: %v", err)
	}

	// Tenant A cannot DELETE Tenant B's unique model
	err = modelStore.DeleteModel(ctx, "tenant-a", "claude-3")
	if !errors.Is(err, ifaces.ErrLLMModelNotFound) {
		t.Fatalf("delete wrong tenant: expected ErrLLMModelNotFound, got: %v", err)
	}

	// Additional: tenant-a creates a second alias, tenant-b should not see it
	mA2 := &ifaces.LLMModel{
		TenantID:      "tenant-a",
		Alias:         "fast-summary",
		ProviderName:  "openai",
		ProviderModel: "gpt-4o-mini",
		Enabled:       true,
	}
	if err := modelStore.CreateModel(ctx, mA2); err != nil {
		t.Fatalf("create model A2: %v", err)
	}
	aModels, err = modelStore.ListModels(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list A after second model: %v", err)
	}
	if len(aModels) != 2 {
		t.Errorf("tenant-a should have 2 models, got %d", len(aModels))
	}
	bModels, err = modelStore.ListModels(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list B after A's second model: %v", err)
	}
	if len(bModels) != 2 {
		t.Errorf("tenant-b should still have 2 models, got %d", len(bModels))
	}
}

func TestLLMModelStore_NotFound(t *testing.T) {
	db := open(t)
	store := db.LLMModels()
	ctx := context.Background()

	_, err := store.GetModel(ctx, "ghost", "nonexistent")
	if !errors.Is(err, ifaces.ErrLLMModelNotFound) {
		t.Fatalf("expected ErrLLMModelNotFound, got: %v", err)
	}

	// List on empty tenant returns empty slice, not error
	list, err := store.ListModels(ctx, "empty-tenant")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestLLMModelStore_Defaults(t *testing.T) {
	db := open(t)
	modelStore := db.LLMModels()
	providerStore := db.LLMProviders()
	ctx := context.Background()

	// Create provider first (FK requirement)
	p := &ifaces.LLMProvider{
		TenantID:   "tenant-x",
		Name:       "custom",
		Driver:     "custom_openai",
		ConfigJSON: `{"base_url": "http://localhost:8080/v1"}`,
		Enabled:    true,
	}
	if err := providerStore.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// DefaultParamsJSON defaults to {}, Capabilities defaults to []
	m := &ifaces.LLMModel{
		TenantID:      "tenant-x",
		Alias:         "fast-model",
		ProviderName:  "custom",
		ProviderModel: "my-model",
		Enabled:       true,
		// DefaultParamsJSON, Capabilities omitted
	}
	if err := modelStore.CreateModel(ctx, m); err != nil {
		t.Fatalf("create with defaults: %v", err)
	}
	got, err := modelStore.GetModel(ctx, "tenant-x", "fast-model")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DefaultParamsJSON != "{}" {
		t.Errorf("DefaultParamsJSON default: got %q, want {}", got.DefaultParamsJSON)
	}
	if got.Capabilities != "[]" {
		t.Errorf("Capabilities default: got %q, want []", got.Capabilities)
	}
}
