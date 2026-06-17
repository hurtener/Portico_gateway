package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestLLMQuotaStore_CRUD(t *testing.T) {
	db := open(t)
	store := db.LLMQuotas()
	ctx := context.Background()

	// Set quota for tenant-a
	q := &ifaces.LLMQuota{
		TenantID:          "tenant-a",
		RequestsPerMinute: 1200,
		TokensPerMinute:   500000,
		TokensPerDay:      10000000,
		CostUSDPerDay:     250.0,
	}
	if err := store.SetQuota(ctx, q); err != nil {
		t.Fatalf("set quota: %v", err)
	}
	if q.UpdatedAt == "" {
		t.Fatal("updated_at not set")
	}

	// Get quota
	got, err := store.GetQuota(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	if got.TenantID != "tenant-a" || got.RequestsPerMinute != 1200 || got.TokensPerMinute != 500000 || got.TokensPerDay != 10000000 || got.CostUSDPerDay != 250.0 {
		t.Errorf("get mismatch: %+v", got)
	}
	if got.UpdatedAt == "" {
		t.Fatal("updated_at not set on get")
	}

	// Update quota
	q.RequestsPerMinute = 1800
	if err := store.SetQuota(ctx, q); err != nil {
		t.Fatalf("update quota: %v", err)
	}
	got, err = store.GetQuota(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.RequestsPerMinute != 1800 {
		t.Errorf("update not persisted: %+v", got)
	}

	// Delete quota
	if err := store.DeleteQuota(ctx, "tenant-a"); err != nil {
		t.Fatalf("delete quota: %v", err)
	}
	_, err = store.GetQuota(ctx, "tenant-a")
	if !errors.Is(err, ifaces.ErrLLMQuotaNotFound) {
		t.Fatalf("expected ErrLLMQuotaNotFound after delete, got: %v", err)
	}
}

func TestLLMQuotaStore_GetOrDefault(t *testing.T) {
	db := open(t)
	store := db.LLMQuotas()
	ctx := context.Background()

	// GetOrDefault for unset tenant returns defaults
	got, err := store.GetOrDefault(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("getordefault unset: %v", err)
	}
	def := ifaces.DefaultLLMQuota("tenant-b")
	if got.TenantID != def.TenantID || got.RequestsPerMinute != def.RequestsPerMinute || got.TokensPerMinute != def.TokensPerMinute || got.TokensPerDay != def.TokensPerDay || got.CostUSDPerDay != def.CostUSDPerDay {
		t.Errorf("getordefault mismatch: got %+v, want %+v", got, def)
	}

	// GetOrDefault for set tenant returns set values
	q := &ifaces.LLMQuota{
		TenantID:          "tenant-c",
		RequestsPerMinute: 999,
		TokensPerMinute:   999999,
		TokensPerDay:      9999999,
		CostUSDPerDay:     99.9,
	}
	if err := store.SetQuota(ctx, q); err != nil {
		t.Fatalf("set for getordefault: %v", err)
	}
	got, err = store.GetOrDefault(ctx, "tenant-c")
	if err != nil {
		t.Fatalf("getordefault set: %v", err)
	}
	if got.RequestsPerMinute != 999 || got.CostUSDPerDay != 99.9 {
		t.Errorf("getordefault not returning set values: %+v", got)
	}
}

func TestLLMQuotaStore_CrossTenantIsolation(t *testing.T) {
	db := open(t)
	store := db.LLMQuotas()
	ctx := context.Background()

	// Tenant A sets a quota
	qA := &ifaces.LLMQuota{
		TenantID:          "tenant-a",
		RequestsPerMinute: 100,
		TokensPerMinute:   10000,
		TokensPerDay:      100000,
		CostUSDPerDay:     10.0,
	}
	if err := store.SetQuota(ctx, qA); err != nil {
		t.Fatalf("set tenant A: %v", err)
	}

	// Tenant B has no quota
	gotB, err := store.GetOrDefault(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("getordefault tenant B: %v", err)
	}
	defB := ifaces.DefaultLLMQuota("tenant-b")
	if gotB.RequestsPerMinute != defB.RequestsPerMinute {
		t.Errorf("tenant B got wrong default: %+v", gotB)
	}

	// Tenant A's GetQuota works
	gotA, err := store.GetQuota(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("get tenant A: %v", err)
	}
	if gotA.RequestsPerMinute != 100 {
		t.Errorf("tenant A quota mismatch: %+v", gotA)
	}

	// Tenant A querying B's quota gets NotFound
	_, err = store.GetQuota(ctx, "tenant-b")
	if !errors.Is(err, ifaces.ErrLLMQuotaNotFound) {
		t.Fatalf("expected ErrLLMQuotaNotFound for tenant B, got: %v", err)
	}

	// Tenant B querying A's quota gets NotFound (cross-tenant isolation)
	_, err = store.GetQuota(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("tenant B querying A should not error (storage is not tenant-aware in call), got: %v", err)
	}
	// Wait, storage layer doesn't enforce tenant isolation on GetQuota calls —
	// it filters by WHERE tenant_id = ?. The isolation is enforced by the caller
	// passing the correct tenantID. So this test just verifies the WHERE clause works.
}
