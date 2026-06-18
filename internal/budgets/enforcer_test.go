package budgets_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/budgets"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func newBudgetStore(t *testing.T) ifaces.BudgetStore {
	t.Helper()
	db, err := sqlite.Open(context.Background(), ":memory:", slog.Default())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.Budgets()
}

func putCostBudget(t *testing.T, s ifaces.BudgetStore, id, kind, scopeID string, limit float64) {
	t.Helper()
	if err := s.PutBudget(context.Background(), &ifaces.Budget{
		TenantID: "t", ID: id, ScopeKind: kind, ScopeID: scopeID,
		Metric: budgets.MetricCostUSD, Period: "1h", Alignment: "rolling",
		LimitVal: limit, Enabled: true,
	}); err != nil {
		t.Fatalf("put budget %s: %v", id, err)
	}
}

var refNow = time.Date(2026, 5, 12, 13, 30, 0, 0, time.UTC)

func cost(c float64) budgets.Usage { return budgets.Usage{Requests: 1, CostUSD: c} }

func TestEnforcer_VKLevelFires(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 1.00)

	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}}
	// Spend 0.80, then a 0.30 call would push to 1.10 > 1.00.
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(0.80)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	v, err := e.PreCheck(ctx, "t", scopes, refNow, cost(0.30))
	if err != nil {
		t.Fatalf("precheck: %v", err)
	}
	if v == nil || v.Level != "vk" || v.Metric != budgets.MetricCostUSD {
		t.Fatalf("want vk cost_usd violation, got %+v", v)
	}
	// A small call that stays under the limit passes.
	if v, _ := e.PreCheck(ctx, "t", scopes, refNow, cost(0.10)); v != nil {
		t.Fatalf("0.80+0.10 should be under 1.00, got violation %+v", v)
	}
}

func TestEnforcer_TeamLevelFires(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 10.00) // VK has plenty of headroom
	putCostBudget(t, s, "b-team", "team", "tm1", 1.00)

	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}, {Kind: "team", ID: "tm1"}}
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(0.80)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	v, err := e.PreCheck(ctx, "t", scopes, refNow, cost(0.30))
	if err != nil {
		t.Fatalf("precheck: %v", err)
	}
	if v == nil || v.Level != "team" {
		t.Fatalf("want team-level violation (VK has headroom), got %+v", v)
	}
}

func TestEnforcer_CustomerLevelFires(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 10.00)
	putCostBudget(t, s, "b-cust", "customer", "c1", 1.00)

	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}, {Kind: "customer", ID: "c1"}}
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(0.90)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	v, _ := e.PreCheck(ctx, "t", scopes, refNow, cost(0.20))
	if v == nil || v.Level != "customer" {
		t.Fatalf("want customer-level violation, got %+v", v)
	}
}

func TestEnforcer_LowestLevelWins(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 1.00)
	putCostBudget(t, s, "b-team", "team", "tm1", 1.00)

	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}, {Kind: "team", ID: "tm1"}}
	// Both ledgers reach 0.80; a 0.30 call would trip BOTH — report the most
	// specific (vk).
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(0.80)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	v, _ := e.PreCheck(ctx, "t", scopes, refNow, cost(0.30))
	if v == nil || v.Level != "vk" {
		t.Fatalf("when both fire, want vk (most specific), got %+v", v)
	}
}

func TestEnforcer_ReconcileUpdatesAllLevels(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 100)
	putCostBudget(t, s, "b-team", "team", "tm1", 100)
	putCostBudget(t, s, "b-cust", "customer", "c1", 100)

	scopes := []budgets.Scope{
		{Kind: "vk", ID: "vk1"}, {Kind: "team", ID: "tm1"}, {Kind: "customer", ID: "c1"},
	}
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(0.50)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	win, _ := budgets.WindowFor(refNow, "1h", "rolling")
	for _, id := range []string{"b-vk", "b-team", "b-cust"} {
		le, ok, err := s.GetLedger(ctx, "t", id, win.Key)
		if err != nil || !ok {
			t.Fatalf("ledger %s: ok=%v err=%v", id, ok, err)
		}
		if le.Used != 0.50 {
			t.Errorf("ledger %s used: want 0.50, got %v", id, le.Used)
		}
	}
}

func TestEnforcer_WarningsDebounced(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 1.00)
	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}}

	// Cross 80%: one warning at 80.
	ws, err := e.Reconcile(ctx, "t", scopes, refNow, cost(0.80))
	if err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	if len(ws) != 1 || ws[0].Pct != 80 {
		t.Fatalf("want one 80%% warning, got %+v", ws)
	}
	// Still within 80–95% band, same window: NO duplicate warning.
	ws, _ = e.Reconcile(ctx, "t", scopes, refNow, cost(0.05)) // 0.85
	if len(ws) != 0 {
		t.Fatalf("want no duplicate warning at 85%%, got %+v", ws)
	}
	// Cross 95%: a new warning at 95.
	ws, _ = e.Reconcile(ctx, "t", scopes, refNow, cost(0.12)) // 0.97
	if len(ws) != 1 || ws[0].Pct != 95 {
		t.Fatalf("want one 95%% warning, got %+v", ws)
	}
	// Cross 100%: a new warning at 100.
	ws, _ = e.Reconcile(ctx, "t", scopes, refNow, cost(0.10)) // 1.07
	if len(ws) != 1 || ws[0].Pct != 100 {
		t.Fatalf("want one 100%% warning, got %+v", ws)
	}
}

func TestEnforcer_NoBudgetsNoViolation(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}}
	if v, err := e.PreCheck(ctx, "t", scopes, refNow, cost(99)); err != nil || v != nil {
		t.Fatalf("no budgets → no violation, got v=%+v err=%v", v, err)
	}
	if ws, err := e.Reconcile(ctx, "t", scopes, refNow, cost(99)); err != nil || len(ws) != 0 {
		t.Fatalf("no budgets → no warnings, got %+v err=%v", ws, err)
	}
}

func TestEnforcer_DisabledBudgetIgnored(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	if err := s.PutBudget(ctx, &ifaces.Budget{
		TenantID: "t", ID: "b1", ScopeKind: "vk", ScopeID: "vk1",
		Metric: budgets.MetricCostUSD, Period: "1h", Alignment: "rolling",
		LimitVal: 0.01, Enabled: false,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if v, _ := e.PreCheck(ctx, "t", []budgets.Scope{{Kind: "vk", ID: "vk1"}}, refNow, cost(99)); v != nil {
		t.Fatalf("disabled budget must not fire, got %+v", v)
	}
}
