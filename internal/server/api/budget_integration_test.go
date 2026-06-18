package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/budgets"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func budgetTestDeps(t *testing.T) (Deps, ifaces.BudgetStore, ifaces.GovernanceStore) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), ":memory:", slog.Default())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bs := db.Budgets()
	gs := db.Governance()
	return Deps{
		Budgets:        bs,
		Governance:     gs,
		BudgetEnforcer: budgets.NewEnforcer(bs),
	}, bs, gs
}

func reqWithVK(vk *virtualkeys.Resolved) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if vk != nil {
		r = r.WithContext(virtualkeys.WithResolved(r.Context(), vk))
	}
	return r
}

func TestBudgetScopeChain(t *testing.T) {
	d, _, gs := budgetTestDeps(t)
	ctx := context.Background()
	// A team that belongs to a customer (customer must exist first — FK).
	if err := gs.PutCustomer(ctx, &ifaces.Customer{TenantID: "t", ID: "c1", Name: "cust"}); err != nil {
		t.Fatalf("put customer: %v", err)
	}
	if err := gs.PutTeam(ctx, &ifaces.Team{TenantID: "t", ID: "tm1", CustomerID: "c1", Name: "team"}); err != nil {
		t.Fatalf("put team: %v", err)
	}

	// VK → team → (team's customer) → tenant.
	chain := budgetScopeChain(ctx, d, "t", &virtualkeys.Resolved{VKID: "vk1", TenantID: "t", ParentKind: "team", ParentID: "tm1"})
	if got := kinds(chain); got != "vk,team,customer,tenant" {
		t.Fatalf("team chain: got %s", got)
	}
	// VK → customer → tenant.
	chain = budgetScopeChain(ctx, d, "t", &virtualkeys.Resolved{VKID: "vk1", TenantID: "t", ParentKind: "customer", ParentID: "c9"})
	if got := kinds(chain); got != "vk,customer,tenant" {
		t.Fatalf("customer chain: got %s", got)
	}
	// No VK (JWT caller) → tenant only.
	chain = budgetScopeChain(ctx, d, "t", nil)
	if got := kinds(chain); got != "tenant" {
		t.Fatalf("jwt chain: got %s", got)
	}
}

func kinds(c []budgets.Scope) string {
	out := ""
	for i, s := range c {
		if i > 0 {
			out += ","
		}
		out += s.Kind
	}
	return out
}

func TestCheckBudget_FiresAtVKLevel(t *testing.T) {
	d, bs, _ := budgetTestDeps(t)
	ctx := context.Background()
	// A 1-request/hour VK budget, already spent.
	if err := bs.PutBudget(ctx, &ifaces.Budget{
		TenantID: "t", ID: "b1", ScopeKind: "vk", ScopeID: "vk1",
		Metric: budgets.MetricRequests, Period: "1h", Alignment: "rolling",
		LimitVal: 1, Enabled: true,
	}); err != nil {
		t.Fatalf("put budget: %v", err)
	}
	vk := &virtualkeys.Resolved{VKID: "vk1", TenantID: "t"}
	// Spend the one allowed request (same rolling window checkBudget will use).
	if _, err := d.BudgetEnforcer.Reconcile(ctx, "t",
		budgetScopeChain(ctx, d, "t", vk), time.Now().UTC(), budgets.Usage{Requests: 1}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	w := httptest.NewRecorder()
	ok := checkBudget(d, w, reqWithVK(vk), "t", "anthropic", "claude", 0)
	if ok {
		t.Fatal("second request should be denied (budget exhausted)")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}
	var body struct {
		Error  string `json:"error"`
		Reason struct {
			Level  string `json:"level"`
			Metric string `json:"metric"`
		} `json:"details"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "budget_exceeded" {
		t.Fatalf("want budget_exceeded, got %q (body=%s)", body.Error, w.Body.String())
	}
}

func TestVirtualKeyBudgetHandler(t *testing.T) {
	d, bs, gs := budgetTestDeps(t)
	ctx := context.Background()
	// A VK + a vk-level budget under tenant t1 (newReq uses tenant t1).
	if err := gs.PutVirtualKey(ctx, &ifaces.VirtualKey{
		TenantID: "t1", ID: "vk1", Name: "k", Salt: []byte{1}, HMAC: []byte{2}, Enabled: true,
	}); err != nil {
		t.Fatalf("put vk: %v", err)
	}
	if err := bs.PutBudget(ctx, &ifaces.Budget{
		TenantID: "t1", ID: "b1", ScopeKind: "vk", ScopeID: "vk1",
		Metric: budgets.MetricCostUSD, Period: "1h", Alignment: "rolling", LimitVal: 5, Enabled: true,
	}); err != nil {
		t.Fatalf("put budget: %v", err)
	}

	r := withChiURLParam(newReq("GET", "/api/governance/virtual-keys/vk1/budget", nil), "id", "vk1")
	w := runHandler(virtualKeyBudgetHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		VKID   string                `json:"vk_id"`
		Levels []budgets.LevelStatus `json:"levels"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.VKID != "vk1" || len(body.Levels) != 1 || body.Levels[0].Level != "vk" {
		t.Fatalf("budget read wrong: %s", w.Body.String())
	}

	// Missing VK → 404.
	r404 := withChiURLParam(newReq("GET", "/x/nope/budget", nil), "id", "nope")
	if w := runHandler(virtualKeyBudgetHandler(d), r404); w.Code != http.StatusNotFound {
		t.Fatalf("missing VK: want 404, got %d", w.Code)
	}
}

func TestCheckBudget_NoEnforcerAllows(t *testing.T) {
	w := httptest.NewRecorder()
	if !checkBudget(Deps{}, w, reqWithVK(nil), "t", "anthropic", "claude", 0) {
		t.Fatal("no enforcer must allow")
	}
}

func TestCheckBudget_HeadroomAllows(t *testing.T) {
	d, bs, _ := budgetTestDeps(t)
	ctx := context.Background()
	if err := bs.PutBudget(ctx, &ifaces.Budget{
		TenantID: "t", ID: "b1", ScopeKind: "tenant", ScopeID: "t",
		Metric: budgets.MetricRequests, Period: "1h", Alignment: "rolling",
		LimitVal: 100, Enabled: true,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	w := httptest.NewRecorder()
	if !checkBudget(d, w, reqWithVK(nil), "t", "anthropic", "claude", 0) {
		t.Fatalf("under-limit request should pass; got %d %s", w.Code, w.Body.String())
	}
}
