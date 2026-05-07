package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type fakeApprovalStore struct {
	mu   sync.Mutex
	rows map[string]*ifaces.ApprovalRecord
}

func newFakeStore() *fakeApprovalStore {
	return &fakeApprovalStore{rows: make(map[string]*ifaces.ApprovalRecord)}
}

func (f *fakeApprovalStore) Insert(_ context.Context, a *ifaces.ApprovalRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[a.ID] = a
	return nil
}

func (f *fakeApprovalStore) Get(_ context.Context, _ string, id string) (*ifaces.ApprovalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return nil, errNotFound{}
	}
	return r, nil
}

type errNotFound struct{}

func (errNotFound) Error() string { return "not found" }

func reqWithTenant(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	id := tenant.Identity{TenantID: "tenant-a", UserID: "alice", Scopes: []string{"admin"}}
	ctx := tenant.With(r.Context(), id)
	return r.WithContext(ctx)
}

func TestApprovalGate_NoHeader_Returns202AndInsertsRow(t *testing.T) {
	store := newFakeStore()
	emitter := &audit.SliceEmitter{}
	mw := NewApprovalGate(Config{Store: store, Audit: emitter, Verb: "tenant.delete", Timeout: time.Minute})

	called := false
	gated := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	w := httptest.NewRecorder()
	gated.ServeHTTP(w, reqWithTenant("DELETE", "/api/admin/tenants/foo"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
	if called {
		t.Fatalf("inner handler should not be called")
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["approval_request_id"] == nil {
		t.Fatalf("missing approval_request_id; body=%s", w.Body.String())
	}
	if len(emitter.Events()) != 1 {
		t.Fatalf("expected one audit event, got %d", len(emitter.Events()))
	}
}

func TestApprovalGate_ApprovedToken_PassesThrough(t *testing.T) {
	store := newFakeStore()
	mw := NewApprovalGate(Config{Store: store, Verb: "x"})

	// Pre-seed approved row.
	id := "appr_test"
	_ = store.Insert(context.Background(), &ifaces.ApprovalRecord{
		ID:        id,
		TenantID:  "tenant-a",
		Status:    approval.StatusApproved,
		CreatedAt: time.Now().UTC(),
	})

	called := false
	gated := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	r := reqWithTenant("DELETE", "/x")
	r.Header.Set(HeaderApprovalToken, id)
	w := httptest.NewRecorder()
	gated.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Fatalf("inner handler not invoked")
	}
}

func TestApprovalGate_PendingToken_403(t *testing.T) {
	store := newFakeStore()
	mw := NewApprovalGate(Config{Store: store, Verb: "x"})
	id := "appr_pending"
	_ = store.Insert(context.Background(), &ifaces.ApprovalRecord{
		ID:        id,
		TenantID:  "tenant-a",
		Status:    approval.StatusPending,
		CreatedAt: time.Now().UTC(),
	})
	gated := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatalf("inner handler should not be called")
	}))
	r := reqWithTenant("DELETE", "/x")
	r.Header.Set(HeaderApprovalToken, id)
	w := httptest.NewRecorder()
	gated.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestApprovalGate_UnknownToken_403(t *testing.T) {
	store := newFakeStore()
	mw := NewApprovalGate(Config{Store: store, Verb: "x"})
	gated := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	r := reqWithTenant("DELETE", "/x")
	r.Header.Set(HeaderApprovalToken, "appr_does_not_exist")
	w := httptest.NewRecorder()
	gated.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
