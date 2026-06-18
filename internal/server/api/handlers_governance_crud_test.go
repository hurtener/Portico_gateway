package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// openGovDB opens a fresh in-memory SQLite DB for governance CRUD tests.
// The migrations create the governance_customers / governance_teams /
// governance_budgets tables; .Governance() and .Budgets() return the live
// stores the handlers exercise.
func openGovDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "gov-crud.db") + "?cache=shared"
	db, err := sqlite.Open(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newGovRouterDeps builds a Deps with DevMode tenant "t1" + admin scope, the
// in-memory governance + budget stores wired, and mounts NewRouter. Returns
// the mounted handler so each test can issue real HTTP requests.
func newGovRouterDeps(t *testing.T) (http.Handler, *sqlite.DB) {
	t.Helper()
	db := openGovDB(t)
	d := Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		DevMode:    true,
		DevTenant:  "t1",
		Governance: db.Governance(),
		Budgets:    db.Budgets(),
	}
	return NewRouter(d), db
}

// doReq is a thin wrapper that issues a request against the mounted router
// and returns the recorder. body may be nil.
func doReq(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rdr)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// govDecode unmarshals the recorder body into v.
func govDecode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v: body=%s", err, w.Body.String())
	}
}

// TestGovernanceCustomers_CRUD covers create→get→list→update→delete for
// customers, plus 404 on a missing get/update/delete.
func TestGovernanceCustomers_CRUD(t *testing.T) {
	h, _ := newGovRouterDeps(t)

	// Create.
	w := doReq(t, h, "POST", "/api/governance/customers", map[string]any{
		"name":        "Acme",
		"description": "Anvil retailer",
		"webhook_url": "https://acme.example/hook",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var created CustomerDTO
	govDecode(t, w, &created)
	if created.ID == "" || created.Name != "Acme" || created.Description != "Anvil retailer" || created.WebhookURL != "https://acme.example/hook" {
		t.Fatalf("created DTO mismatch: %+v", created)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("timestamps not populated: %+v", created)
	}
	if len(created.ID) < 6 || created.ID[:5] != "cust_" {
		t.Fatalf("id not cust_-prefixed: %s", created.ID)
	}

	// Get.
	w = doReq(t, h, "GET", "/api/governance/customers/"+created.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var got CustomerDTO
	govDecode(t, w, &got)
	if got.ID != created.ID || got.Name != "Acme" {
		t.Fatalf("get mismatch: %+v", got)
	}

	// List includes it.
	w = doReq(t, h, "GET", "/api/governance/customers", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var list []CustomerDTO
	govDecode(t, w, &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", list)
	}

	// Update.
	w = doReq(t, h, "PUT", "/api/governance/customers/"+created.ID, map[string]any{
		"name":        "Acme Corp",
		"description": "Updated",
		"webhook_url": "https://acme.example/v2",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var updated CustomerDTO
	govDecode(t, w, &updated)
	if updated.Name != "Acme Corp" || updated.Description != "Updated" || updated.WebhookURL != "https://acme.example/v2" {
		t.Fatalf("update did not round-trip: %+v", updated)
	}
	if updated.CreatedAt != got.CreatedAt {
		t.Fatalf("created_at changed on update: was %s now %s", got.CreatedAt, updated.CreatedAt)
	}

	// Delete → 204; subsequent get → 404.
	w = doReq(t, h, "DELETE", "/api/governance/customers/"+created.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	w = doReq(t, h, "GET", "/api/governance/customers/"+created.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get-after-delete status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceCustomers_NotFoundOnUpdate covers 404 for update on a
// missing id and a missing-name validation error on create.
func TestGovernanceCustomers_NotFoundOnUpdate(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "PUT", "/api/governance/customers/cust_nope", map[string]any{"name": "X"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceCustomers_CreateRequiresName covers the 400 path.
func TestGovernanceCustomers_CreateRequiresName(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "POST", "/api/governance/customers", map[string]any{"description": "no name"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create without name status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "invalid_request" {
		t.Fatalf("error code: want invalid_request, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceCustomers_DeleteNotFound covers 404 on delete.
func TestGovernanceCustomers_DeleteNotFound(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "DELETE", "/api/governance/customers/cust_nope", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

// TestGovernanceTeams_CRUD covers create→get→list→update→delete for teams,
// including customer_id linkage and 404 on a missing get/update/delete.
func TestGovernanceTeams_CRUD(t *testing.T) {
	h, _ := newGovRouterDeps(t)

	// Seed a customer so we can link the team to it.
	w := doReq(t, h, "POST", "/api/governance/customers", map[string]any{"name": "Cust"})
	if w.Code != http.StatusCreated {
		t.Fatalf("seed customer status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var cust CustomerDTO
	govDecode(t, w, &cust)

	// Create team linked to the customer.
	w = doReq(t, h, "POST", "/api/governance/teams", map[string]any{
		"name":        "Platform",
		"customer_id": cust.ID,
		"description": "Platform team",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create team status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var created TeamDTO
	govDecode(t, w, &created)
	if created.ID == "" || created.Name != "Platform" || created.CustomerID != cust.ID || created.Description != "Platform team" {
		t.Fatalf("created team DTO mismatch: %+v", created)
	}
	if len(created.ID) < 6 || created.ID[:5] != "team_" {
		t.Fatalf("id not team_-prefixed: %s", created.ID)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("timestamps not populated: %+v", created)
	}

	// Get.
	w = doReq(t, h, "GET", "/api/governance/teams/"+created.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get team status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var got TeamDTO
	govDecode(t, w, &got)
	if got.ID != created.ID || got.CustomerID != cust.ID {
		t.Fatalf("get team mismatch: %+v", got)
	}

	// List includes it.
	w = doReq(t, h, "GET", "/api/governance/teams", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list teams status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var list []TeamDTO
	govDecode(t, w, &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list teams mismatch: %+v", list)
	}

	// Update — clear customer_id, change name + description.
	w = doReq(t, h, "PUT", "/api/governance/teams/"+created.ID, map[string]any{
		"name":        "Platform Eng",
		"description": "Updated desc",
		"customer_id": "",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update team status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var updated TeamDTO
	govDecode(t, w, &updated)
	if updated.Name != "Platform Eng" || updated.Description != "Updated desc" || updated.CustomerID != "" {
		t.Fatalf("update team did not round-trip: %+v", updated)
	}

	// Delete → 204; subsequent get → 404.
	w = doReq(t, h, "DELETE", "/api/governance/teams/"+created.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete team status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	w = doReq(t, h, "GET", "/api/governance/teams/"+created.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get team after delete status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

// TestGovernanceTeams_NotFoundOnUpdate covers 404 for update on a missing id.
func TestGovernanceTeams_NotFoundOnUpdate(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "PUT", "/api/governance/teams/team_nope", map[string]any{"name": "X"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing team status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceTeams_CreateRequiresName covers the 400 path.
func TestGovernanceTeams_CreateRequiresName(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "POST", "/api/governance/teams", map[string]any{"description": "no name"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create team without name status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "invalid_request" {
		t.Fatalf("error code: want invalid_request, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceBudgets_CRUD covers create→get→list→update→delete for
// budgets, including the default-alignment="rolling" rule and 404 paths.
func TestGovernanceBudgets_CRUD(t *testing.T) {
	h, _ := newGovRouterDeps(t)

	// Create — alignment omitted, must default to "rolling".
	w := doReq(t, h, "POST", "/api/governance/budgets", map[string]any{
		"scope_kind": "tenant",
		"scope_id":   "t1",
		"metric":     "cost_usd",
		"period":     "1d",
		"limit_val":  12.50,
		"enabled":    true,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create budget status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var created BudgetDTO
	govDecode(t, w, &created)
	if created.ID == "" || created.ScopeKind != "tenant" || created.ScopeID != "t1" || created.Metric != "cost_usd" || created.Period != "1d" {
		t.Fatalf("created budget DTO mismatch: %+v", created)
	}
	if created.Alignment != "rolling" {
		t.Fatalf("default alignment: want rolling, got %q", created.Alignment)
	}
	if created.LimitVal != 12.50 || !created.Enabled {
		t.Fatalf("limit_val/enabled mismatch: %+v", created)
	}
	if len(created.ID) < 5 || created.ID[:4] != "bdg_" {
		t.Fatalf("id not bdg_-prefixed: %s", created.ID)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("timestamps not populated: %+v", created)
	}

	// Get.
	w = doReq(t, h, "GET", "/api/governance/budgets/"+created.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get budget status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var got BudgetDTO
	govDecode(t, w, &got)
	if got.ID != created.ID || got.Alignment != "rolling" {
		t.Fatalf("get budget mismatch: %+v", got)
	}

	// List includes it.
	w = doReq(t, h, "GET", "/api/governance/budgets", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list budgets status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var list []BudgetDTO
	govDecode(t, w, &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list budgets mismatch: %+v", list)
	}

	// Update — change limit_val, alignment, enabled.
	w = doReq(t, h, "PUT", "/api/governance/budgets/"+created.ID, map[string]any{
		"scope_kind": "tenant",
		"scope_id":   "t1",
		"metric":     "cost_usd",
		"period":     "1d",
		"alignment":  "calendar",
		"limit_val":  99.0,
		"enabled":    false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update budget status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var updated BudgetDTO
	govDecode(t, w, &updated)
	if updated.Alignment != "calendar" || updated.LimitVal != 99.0 || updated.Enabled {
		t.Fatalf("update budget did not round-trip: %+v", updated)
	}
	if updated.CreatedAt != got.CreatedAt {
		t.Fatalf("created_at changed on update: was %s now %s", got.CreatedAt, updated.CreatedAt)
	}

	// Delete → 204; subsequent get → 404.
	w = doReq(t, h, "DELETE", "/api/governance/budgets/"+created.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete budget status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	w = doReq(t, h, "GET", "/api/governance/budgets/"+created.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get budget after delete status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceBudgets_CreateRequiresFields covers the 400 path: every one
// of scope_kind/scope_id/metric/period is required.
func TestGovernanceBudgets_CreateRequiresFields(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "POST", "/api/governance/budgets", map[string]any{
		"scope_kind": "tenant",
		"metric":     "cost_usd",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create budget (missing fields) status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "invalid_request" {
		t.Fatalf("error code: want invalid_request, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceBudgets_NotFoundOnUpdate covers 404 for update on a missing id.
func TestGovernanceBudgets_NotFoundOnUpdate(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "PUT", "/api/governance/budgets/bdg_nope", map[string]any{"limit_val": 1})
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing budget status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestGovernanceBudgets_DeleteNotFound covers 404 on delete.
func TestGovernanceBudgets_DeleteNotFound(t *testing.T) {
	h, _ := newGovRouterDeps(t)
	w := doReq(t, h, "DELETE", "/api/governance/budgets/bdg_nope", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing budget status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

// TestGovernanceCRUD_NilStores503 verifies the handlers degrade to 503 when
// the governance / budget stores are not wired (partially-configured build).
// Mirrors TestAgentProfiles_NilStore503: invoke the handler directly, since
// NewRouter skips mounting the routes when the stores are nil.
func TestGovernanceCRUD_NilStores503(t *testing.T) {
	cases := []struct {
		name string
		h    http.HandlerFunc
		path string
	}{
		{"customers", listCustomersHandler(Deps{}), "/api/governance/customers"},
		{"teams", listTeamsHandler(Deps{}), "/api/governance/teams"},
		{"budgets", listBudgetsHandler(Deps{}), "/api/governance/budgets"},
	}
	for _, c := range cases {
		w := runHandler(c.h, newReq("GET", c.path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503 (%s)", c.name, w.Code, w.Body.String())
		}
		if readErrorCode(t, w) != "governance_not_configured" && readErrorCode(t, w) != "budgets_not_configured" {
			t.Errorf("%s: error code = %q, want *_not_configured", c.name, readErrorCode(t, w))
		}
	}
}
