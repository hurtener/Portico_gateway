// Package consolecrud hosts the Phase 9 carry-over integration tests
// that the Phase 10 plan absorbed. The tests here exercise the Phase 9
// REST surface via the in-process registry / approval / policy seams
// against a real SQLite store. Full HTTP-stack coverage lives in the
// smoke script and the api package's handler tests.
package consolecrud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/server/api/middleware"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func openDB(t *testing.T) *sqlite.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "console-crud.db") + "?cache=shared"
	db, err := sqlite.Open(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestE2E_PolicyEdit_HotReload — adding a deny rule via the rule store
// surfaces in subsequent reads without any process restart.
func TestE2E_PolicyEdit_HotReload(t *testing.T) {
	db := openDB(t)
	store := policy.NewRuleStore(db.PolicyRules())

	rs, err := store.List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rs.Rules) != 0 {
		t.Fatalf("expected empty rules; got %d", len(rs.Rules))
	}

	rule := policy.Rule{
		ID:        "smoke-deny",
		Priority:  10,
		Enabled:   true,
		RiskClass: policy.RiskWrite,
		Conditions: policy.Conditions{
			Match: policy.Match{Tools: []string{"github.delete_repo"}},
		},
		Actions: policy.Actions{Deny: true},
		Notes:   "no destructive ops",
	}
	if _, err := store.Upsert(context.Background(), "tenant-a", rule); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rs, err = store.List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rs.Rules) != 1 || rs.Rules[0].ID != "smoke-deny" {
		t.Fatalf("rule not visible after upsert: %+v", rs.Rules)
	}
}

// TestE2E_DestructiveDelete_RequiresApproval — DELETE of a sensitive
// verb funnels through the approval gate and returns 202.
func TestE2E_DestructiveDelete_RequiresApproval(t *testing.T) {
	store := newMemApprovalStore()
	mw := middleware.NewApprovalGate(middleware.Config{
		Store:   store,
		Audit:   audit.NopEmitter{},
		Verb:    "tenant.delete",
		Timeout: 5 * time.Minute,
	})
	called := false
	gated := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	r := httptest.NewRequest("DELETE", "/api/admin/tenants/foo", nil)
	r = r.WithContext(tenant.With(r.Context(),
		tenant.Identity{TenantID: "tenant-a", UserID: "alice", Scopes: []string{"admin"}}))
	w := httptest.NewRecorder()
	gated.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if called {
		t.Fatalf("inner handler should not run before approval")
	}

	// Approve the row out-of-band, then re-issue with the token header.
	store.mu.Lock()
	for _, a := range store.rows {
		a.Status = approval.StatusApproved
	}
	approvalID := ""
	for id := range store.rows {
		approvalID = id
	}
	store.mu.Unlock()

	r2 := httptest.NewRequest("DELETE", "/api/admin/tenants/foo", nil)
	r2.Header.Set(middleware.HeaderApprovalToken, approvalID)
	r2 = r2.WithContext(tenant.With(r2.Context(),
		tenant.Identity{TenantID: "tenant-a", UserID: "alice", Scopes: []string{"admin"}}))
	w2 := httptest.NewRecorder()
	gated.ServeHTTP(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 after approval, got %d", w2.Code)
	}
	if !called {
		t.Fatalf("expected inner handler to run with approved token")
	}
}

// TestE2E_ServerCRUD_NoRestart — covered by scripts/smoke/phase-9.sh.
// The smoke exercises the live HTTP stack; replicating it here would
// duplicate the assertion. Kept named for traceability.
func TestE2E_ServerCRUD_NoRestart(t *testing.T) {
	t.Skip("reason: covered by scripts/smoke/phase-9.sh full HTTP stack")
}

// memApprovalStore is the in-memory approval store the gate uses.
type memApprovalStore struct {
	mu   sync.Mutex
	rows map[string]*ifaces.ApprovalRecord
}

func newMemApprovalStore() *memApprovalStore {
	return &memApprovalStore{rows: make(map[string]*ifaces.ApprovalRecord)}
}

func (m *memApprovalStore) Insert(_ context.Context, a *ifaces.ApprovalRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[a.ID] = a
	return nil
}

func (m *memApprovalStore) Get(_ context.Context, _, id string) (*ifaces.ApprovalRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.rows[id]; ok {
		return a, nil
	}
	return nil, errMissing{}
}

type errMissing struct{}

func (errMissing) Error() string { return "not found" }
