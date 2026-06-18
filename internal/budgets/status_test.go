package budgets_test

import (
	"context"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/budgets"
)

func TestEnforcer_Headroom(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b-vk", "vk", "vk1", 10.0)
	putCostBudget(t, s, "b-tenant", "tenant", "t", 100.0)

	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}, {Kind: "tenant", ID: "t"}}
	// Spend 2.50 on the VK budget only (tenant scope has its own ledger row too,
	// since both are in the chain Reconcile touches).
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(2.50)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	st, err := e.Headroom(ctx, "t", scopes, refNow)
	if err != nil {
		t.Fatalf("headroom: %v", err)
	}
	if len(st) != 2 {
		t.Fatalf("want 2 levels, got %d", len(st))
	}
	byLevel := map[string]budgets.LevelStatus{}
	for _, l := range st {
		byLevel[l.Level] = l
	}
	vk := byLevel["vk"]
	if vk.Used != 2.50 || vk.Limit != 10.0 {
		t.Fatalf("vk status wrong: %+v", vk)
	}
	// 2.50/10 used → 75% headroom.
	if vk.HeadroomPct < 74.9 || vk.HeadroomPct > 75.1 {
		t.Fatalf("vk headroom: want ~75, got %v", vk.HeadroomPct)
	}
	if vk.ResetsAt == "" {
		t.Fatalf("resets_at not set")
	}
}

func TestHeadroom_OverLimitClampsToZero(t *testing.T) {
	s := newBudgetStore(t)
	e := budgets.NewEnforcer(s)
	ctx := context.Background()
	putCostBudget(t, s, "b", "vk", "vk1", 1.0)
	scopes := []budgets.Scope{{Kind: "vk", ID: "vk1"}}
	if _, err := e.Reconcile(ctx, "t", scopes, refNow, cost(2.0)); err != nil { // over limit
		t.Fatalf("reconcile: %v", err)
	}
	st, _ := e.Headroom(ctx, "t", scopes, refNow)
	if len(st) != 1 || st[0].HeadroomPct != 0 {
		t.Fatalf("over-limit headroom should clamp to 0, got %+v", st)
	}
}
