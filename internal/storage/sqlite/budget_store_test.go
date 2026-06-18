package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func seedBudget(t *testing.T, store ifaces.BudgetStore, tenantID, id, scopeKind, scopeID, metric string, limit float64, enabled bool) {
	t.Helper()
	if err := store.PutBudget(context.Background(), &ifaces.Budget{
		TenantID: tenantID, ID: id, ScopeKind: scopeKind, ScopeID: scopeID,
		Metric: metric, Period: "1h", Alignment: "rolling", LimitVal: limit, Enabled: enabled,
	}); err != nil {
		t.Fatalf("seed budget %s: %v", id, err)
	}
}

func TestBudgetStore_RoundTrip(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	seedBudget(t, store, "t", "b1", "vk", "vk-1", "cost_usd", 1.50, true)
	got, err := store.GetBudget(ctx, "t", "b1")
	if err != nil {
		t.Fatalf("get budget: %v", err)
	}
	if got.ScopeKind != "vk" || got.ScopeID != "vk-1" || got.Metric != "cost_usd" || got.LimitVal != 1.50 {
		t.Errorf("budget mismatch: %+v", got)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("timestamps not set: %+v", got)
	}
	if _, err := store.GetBudget(ctx, "t", "nope"); !errors.Is(err, ifaces.ErrBudgetNotFound) {
		t.Fatalf("want ErrBudgetNotFound, got %v", err)
	}
}

func TestBudgetStore_ListByScope_FiltersEnabled(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	seedBudget(t, store, "t", "b1", "team", "tm-1", "cost_usd", 5, true)
	seedBudget(t, store, "t", "b2", "team", "tm-1", "tokens", 1000, false) // disabled
	seedBudget(t, store, "t", "b3", "team", "tm-2", "cost_usd", 9, true)   // other scope

	got, err := store.ListBudgetsByScope(ctx, "t", "team", "tm-1")
	if err != nil {
		t.Fatalf("list by scope: %v", err)
	}
	if len(got) != 1 || got[0].ID != "b1" {
		t.Fatalf("want only enabled b1, got %+v", got)
	}
}

func TestBudgetStore_GetLedger_AbsentNotError(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	_, ok, err := store.GetLedger(ctx, "t", "b1", "2026-05-12T13")
	if err != nil {
		t.Fatalf("get ledger: %v", err)
	}
	if ok {
		t.Fatalf("want ok=false for absent ledger")
	}
}

func TestBudgetStore_ReconcileUsage_AtomicMultiLevel(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	seedBudget(t, store, "t", "b-vk", "vk", "vk-1", "cost_usd", 10, true)
	seedBudget(t, store, "t", "b-team", "team", "tm-1", "cost_usd", 50, true)

	w := "2026-05-12T13"
	updates := []ifaces.LedgerUpdate{
		{BudgetID: "b-vk", WindowKey: w, Delta: 0.25, ResetsAt: "2026-05-12T14"},
		{BudgetID: "b-team", WindowKey: w, Delta: 0.25, ResetsAt: "2026-05-12T14"},
	}
	if err := store.ReconcileUsage(ctx, "t", updates); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	// Second call ADDS to used.
	if err := store.ReconcileUsage(ctx, "t", updates); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	for _, bid := range []string{"b-vk", "b-team"} {
		le, ok, err := store.GetLedger(ctx, "t", bid, w)
		if err != nil || !ok {
			t.Fatalf("get ledger %s: ok=%v err=%v", bid, ok, err)
		}
		if le.Used != 0.50 {
			t.Errorf("%s used: want 0.50, got %v", bid, le.Used)
		}
		if le.ResetsAt != "2026-05-12T14" {
			t.Errorf("%s resets_at: want set on insert, got %q", bid, le.ResetsAt)
		}
	}
}

func TestBudgetStore_ReconcileUsage_FaultLeavesNoPartial(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	seedBudget(t, store, "t", "b-good", "vk", "vk-1", "cost_usd", 10, true)
	w := "2026-05-12T13"

	// One good update, one referencing a non-existent budget_id (FK violation).
	// The whole transaction must roll back: the good ledger must NOT be written.
	updates := []ifaces.LedgerUpdate{
		{BudgetID: "b-good", WindowKey: w, Delta: 1.0, ResetsAt: "2026-05-12T14"},
		{BudgetID: "b-missing", WindowKey: w, Delta: 1.0, ResetsAt: "2026-05-12T14"},
	}
	if err := store.ReconcileUsage(ctx, "t", updates); err == nil {
		t.Fatalf("expected FK-violation error from reconcile, got nil")
	}
	if _, ok, err := store.GetLedger(ctx, "t", "b-good", w); err != nil {
		t.Fatalf("get ledger: %v", err)
	} else if ok {
		t.Fatalf("partial update: b-good ledger was written despite the failed tx (atomicity broken)")
	}
}

func TestBudgetStore_SetLedgerWarningLevel(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	seedBudget(t, store, "t", "b1", "vk", "vk-1", "cost_usd", 10, true)
	w := "2026-05-12T13"
	if err := store.SetLedgerWarningLevel(ctx, "t", "b1", w, 80); err != nil {
		t.Fatalf("set warning level: %v", err)
	}
	le, ok, err := store.GetLedger(ctx, "t", "b1", w)
	if err != nil || !ok {
		t.Fatalf("get ledger: ok=%v err=%v", ok, err)
	}
	if le.LastWarningLevel != 80 {
		t.Errorf("want last_warning_level=80, got %d", le.LastWarningLevel)
	}
	// Raising it works.
	if err := store.SetLedgerWarningLevel(ctx, "t", "b1", w, 95); err != nil {
		t.Fatalf("raise warning level: %v", err)
	}
	le, _, _ = store.GetLedger(ctx, "t", "b1", w)
	if le.LastWarningLevel != 95 {
		t.Errorf("want last_warning_level=95, got %d", le.LastWarningLevel)
	}
}

func TestBudgetStore_TenantIsolation(t *testing.T) {
	db := open(t)
	store := db.Budgets()
	ctx := context.Background()

	seedBudget(t, store, "tenant-a", "b1", "vk", "vk-1", "cost_usd", 1, true)
	seedBudget(t, store, "tenant-b", "b1", "vk", "vk-1", "cost_usd", 1, true)

	a, err := store.ListBudgets(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(a) != 1 || a[0].TenantID != "tenant-a" {
		t.Fatalf("tenant isolation broken: %+v", a)
	}
}
