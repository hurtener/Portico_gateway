package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestLLMCostStore_UnitCostCRUD(t *testing.T) {
	db := open(t)
	store := db.LLMCosts()
	ctx := context.Background()

	// Set unit cost
	c := &ifaces.LLMUnitCost{
		ProviderDriver: "openai",
		ProviderModel:  "gpt-4o",
		InputPer1K:     2.5,
		OutputPer1K:    10.0,
	}
	if err := store.SetUnitCost(ctx, c); err != nil {
		t.Fatalf("set unit cost: %v", err)
	}

	// Get unit cost
	got, err := store.GetUnitCost(ctx, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("get unit cost: %v", err)
	}
	if got.ProviderDriver != "openai" || got.ProviderModel != "gpt-4o" || got.InputPer1K != 2.5 || got.OutputPer1K != 10.0 {
		t.Errorf("get mismatch: %+v", got)
	}

	// Update unit cost
	c.InputPer1K = 3.0
	if err := store.SetUnitCost(ctx, c); err != nil {
		t.Fatalf("update unit cost: %v", err)
	}
	got, err = store.GetUnitCost(ctx, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.InputPer1K != 3.0 {
		t.Errorf("update not persisted: %+v", got)
	}

	// List unit costs
	list, err := store.ListUnitCosts(ctx)
	if err != nil {
		t.Fatalf("list unit costs: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("list len = %d, want 1", len(list))
	}

	// Get non-existent returns ErrLLMUnitCostNotFound
	_, err = store.GetUnitCost(ctx, "anthropic", "claude-3")
	if !errors.Is(err, ifaces.ErrLLMUnitCostNotFound) {
		t.Fatalf("expected ErrLLMUnitCostNotFound, got: %v", err)
	}
}

func TestLLMCostStore_AddUsage_Accumulates(t *testing.T) {
	db := open(t)
	store := db.LLMCosts()
	ctx := context.Background()

	tenantID := "tenant-a"
	day := "2025-01-15"
	alias := "gpt-4o"

	// First call
	if err := store.AddUsage(ctx, tenantID, day, alias, 5, 1000, 500, 1.25); err != nil {
		t.Fatalf("add usage first: %v", err)
	}

	// Second call for same day+alias (should accumulate)
	if err := store.AddUsage(ctx, tenantID, day, alias, 3, 600, 200, 0.75); err != nil {
		t.Fatalf("add usage second: %v", err)
	}

	// Get daily - should be summed
	list, err := store.ListDaily(ctx, tenantID, "", "")
	if err != nil {
		t.Fatalf("list daily: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 row, got %d", len(list))
	}
	d := list[0]
	if d.TenantID != tenantID || d.Day != day || d.Alias != alias {
		t.Errorf("row identity mismatch: %+v", d)
	}
	if d.Requests != 8 {
		t.Errorf("requests = %d, want 8 (5+3)", d.Requests)
	}
	if d.InputTok != 1600 {
		t.Errorf("input_tok = %d, want 1600 (1000+600)", d.InputTok)
	}
	if d.OutputTok != 700 {
		t.Errorf("output_tok = %d, want 700 (500+200)", d.OutputTok)
	}
	if d.CostUSD != 2.0 {
		t.Errorf("cost_usd = %.4f, want 2.0 (1.25+0.75)", d.CostUSD)
	}
}

func TestLLMCostStore_ListDaily_TenantFiltered(t *testing.T) {
	db := open(t)
	store := db.LLMCosts()
	ctx := context.Background()

	// Add data for tenant-a
	if err := store.AddUsage(ctx, "tenant-a", "2025-01-15", "gpt-4o", 10, 1000, 500, 2.5); err != nil {
		t.Fatalf("add tenant-a: %v", err)
	}
	if err := store.AddUsage(ctx, "tenant-a", "2025-01-16", "gpt-4o", 5, 500, 200, 1.25); err != nil {
		t.Fatalf("add tenant-a day 2: %v", err)
	}

	// Add data for tenant-b
	if err := store.AddUsage(ctx, "tenant-b", "2025-01-15", "claude-3", 8, 800, 400, 2.0); err != nil {
		t.Fatalf("add tenant-b: %v", err)
	}

	// ListDaily for tenant-a should only return tenant-a's rows
	listA, err := store.ListDaily(ctx, "tenant-a", "", "")
	if err != nil {
		t.Fatalf("list tenant-a: %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("tenant-a rows = %d, want 2", len(listA))
	}
	for _, d := range listA {
		if d.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak in list: %+v", d)
		}
	}

	// ListDaily for tenant-b should only return tenant-b's rows
	listB, err := store.ListDaily(ctx, "tenant-b", "", "")
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("tenant-b rows = %d, want 1", len(listB))
	}
	if listB[0].TenantID != "tenant-b" {
		t.Errorf("cross-tenant leak in list: %+v", listB[0])
	}
}

func TestLLMCostStore_ListDaily_RangeFilter(t *testing.T) {
	db := open(t)
	store := db.LLMCosts()
	ctx := context.Background()

	if err := store.AddUsage(ctx, "tenant-a", "2025-01-10", "gpt-4o", 1, 100, 50, 0.1); err != nil {
		t.Fatalf("add 10: %v", err)
	}
	if err := store.AddUsage(ctx, "tenant-a", "2025-01-15", "gpt-4o", 1, 100, 50, 0.1); err != nil {
		t.Fatalf("add 15: %v", err)
	}
	if err := store.AddUsage(ctx, "tenant-a", "2025-01-20", "gpt-4o", 1, 100, 50, 0.1); err != nil {
		t.Fatalf("add 20: %v", err)
	}

	// Range filter
	list, err := store.ListDaily(ctx, "tenant-a", "2025-01-12", "2025-01-18")
	if err != nil {
		t.Fatalf("list range: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("range rows = %d, want 1 (only 2025-01-15)", len(list))
	}
	if list[0].Day != "2025-01-15" {
		t.Errorf("range day = %s, want 2025-01-15", list[0].Day)
	}

	// From only
	list, err = store.ListDaily(ctx, "tenant-a", "2025-01-15", "")
	if err != nil {
		t.Fatalf("list from: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("from rows = %d, want 2 (15, 20)", len(list))
	}

	// To only
	list, err = store.ListDaily(ctx, "tenant-a", "", "2025-01-15")
	if err != nil {
		t.Fatalf("list to: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("to rows = %d, want 2 (10, 15)", len(list))
	}
}

func TestLLMCostStore_CrossTenantIsolation(t *testing.T) {
	db := open(t)
	store := db.LLMCosts()
	ctx := context.Background()

	// Tenant A adds usage
	if err := store.AddUsage(ctx, "tenant-a", "2025-01-15", "gpt-4o", 10, 1000, 500, 2.5); err != nil {
		t.Fatalf("add tenant A: %v", err)
	}

	// Tenant B lists daily - should be empty
	listB, err := store.ListDaily(ctx, "tenant-b", "", "")
	if err != nil {
		t.Fatalf("list tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Errorf("tenant B should see 0 rows, got %d", len(listB))
	}

	// Tenant A lists daily - should see their row
	listA, err := store.ListDaily(ctx, "tenant-a", "", "")
	if err != nil {
		t.Fatalf("list tenant A: %v", err)
	}
	if len(listA) != 1 {
		t.Errorf("tenant A should see 1 row, got %d", len(listA))
	}

	// Tenant A's unit costs are global - tenant B can see them
	if err := store.SetUnitCost(ctx, &ifaces.LLMUnitCost{
		ProviderDriver: "openai",
		ProviderModel:  "gpt-4o-mini",
		InputPer1K:     0.15,
		OutputPer1K:    0.6,
	}); err != nil {
		t.Fatalf("set unit cost: %v", err)
	}

	got, err := store.GetUnitCost(ctx, "openai", "gpt-4o-mini")
	if err != nil {
		t.Fatalf("get unit cost from tenant B context: %v", err)
	}
	if got.ProviderModel != "gpt-4o-mini" {
		t.Errorf("unit cost not globally visible: %+v", got)
	}
}
