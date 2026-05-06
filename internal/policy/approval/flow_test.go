package approval_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
)

// inMemStore is a Store implementation backed by a map. Faster than the
// SQLite path for unit tests.
type inMemStore struct {
	mu sync.Mutex
	m  map[string]*approval.Approval
}

func newInMemStore() *inMemStore { return &inMemStore{m: map[string]*approval.Approval{}} }

func (s *inMemStore) Insert(_ context.Context, a *approval.Approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *a
	s.m[a.ID] = &cp
	return nil
}
func (s *inMemStore) UpdateStatus(_ context.Context, tenantID, id, status string, decidedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[id]
	if !ok || a.TenantID != tenantID {
		return errors.New("not found")
	}
	a.Status = status
	a.DecidedAt = &decidedAt
	return nil
}
func (s *inMemStore) Get(_ context.Context, tenantID, id string) (*approval.Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.m[id]
	if !ok || a.TenantID != tenantID {
		return nil, errors.New("not found")
	}
	cp := *a
	return &cp, nil
}
func (s *inMemStore) ListPending(_ context.Context, tenantID string) ([]*approval.Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*approval.Approval{}
	for _, a := range s.m {
		if a.TenantID == tenantID && a.Status == approval.StatusPending {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (s *inMemStore) ExpireOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, a := range s.m {
		if a.Status == approval.StatusPending && a.ExpiresAt.Before(cutoff) {
			a.Status = approval.StatusExpired
			a.DecidedAt = &cutoff
			n++
		}
	}
	return n, nil
}

// fixedSender returns whatever scripted reply the test sets up.
type fixedSender struct {
	mu    sync.Mutex
	reply *protocol.ElicitationCreateResult
	err   error
	calls int
}

func (f *fixedSender) Elicit(_ context.Context, _ string, _ protocol.ElicitationCreateParams, _ time.Duration) (*protocol.ElicitationCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.reply, f.err
}

type fixedSessions struct {
	hasElicit bool
}

func (f fixedSessions) HasElicitation(_ string) bool { return f.hasElicit }

func newFlow(t *testing.T, sender approval.Sender, sessions approval.SessionLookup) (*approval.Flow, *inMemStore, *audit.SliceEmitter) {
	t.Helper()
	store := newInMemStore()
	em := &audit.SliceEmitter{}
	f := approval.New(store, sender, sessions, em, nil)
	return f, store, em
}

func TestFlow_Elicit_Approved(t *testing.T) {
	sender := &fixedSender{reply: &protocol.ElicitationCreateResult{
		Action:  protocol.ElicitActionAccept,
		Content: json.RawMessage(`{"approve":true}`),
	}}
	f, store, em := newFlow(t, sender, fixedSessions{hasElicit: true})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskExternalSideEffect, ApprovalTimeout: time.Second}
	out, err := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x", RiskClass: dec.RiskClass})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != approval.StatusApproved {
		t.Errorf("decision = %q, want approved", out.Decision)
	}
	if a := out.Approval; a == nil || a.Status != approval.StatusApproved {
		t.Errorf("approval not marked approved: %+v", a)
	}
	// Audit emitted pending + decided.
	got := em.Events()
	if len(got) != 2 {
		t.Fatalf("expected 2 events; got %d (%+v)", len(got), got)
	}
	if got[0].Type != audit.EventApprovalPending || got[1].Type != audit.EventApprovalDecided {
		t.Errorf("unexpected events: %+v", got)
	}
	// Store has one approved row.
	pending, _ := store.ListPending(context.Background(), "acme")
	if len(pending) != 0 {
		t.Errorf("expected no pending approvals; got %d", len(pending))
	}
}

func TestFlow_Elicit_Denied(t *testing.T) {
	sender := &fixedSender{reply: &protocol.ElicitationCreateResult{
		Action:  protocol.ElicitActionAccept,
		Content: json.RawMessage(`{"approve":false}`),
	}}
	f, _, _ := newFlow(t, sender, fixedSessions{hasElicit: true})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskDestructive, ApprovalTimeout: time.Second}
	out, err := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision != approval.StatusDenied {
		t.Errorf("decision = %q, want denied", out.Decision)
	}
}

func TestFlow_Elicit_RejectAction(t *testing.T) {
	sender := &fixedSender{reply: &protocol.ElicitationCreateResult{Action: protocol.ElicitActionReject}}
	f, _, _ := newFlow(t, sender, fixedSessions{hasElicit: true})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskDestructive, ApprovalTimeout: time.Second}
	out, _ := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x"})
	if out.Decision != approval.StatusDenied {
		t.Errorf("decision = %q, want denied", out.Decision)
	}
}

func TestFlow_Elicit_TimeoutMarksExpired(t *testing.T) {
	sender := &fixedSender{err: errors.New("server-initiated: timeout")}
	f, store, _ := newFlow(t, sender, fixedSessions{hasElicit: true})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskDestructive, ApprovalTimeout: time.Millisecond}
	out, _ := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x"})
	if out.Decision != approval.StatusExpired {
		t.Errorf("decision = %q, want expired", out.Decision)
	}
	got, _ := store.Get(context.Background(), "acme", out.Approval.ID)
	if got == nil || got.Status != approval.StatusExpired {
		t.Errorf("store did not mark expired: %+v", got)
	}
}

func TestFlow_NoElicitation_FallbackRequired(t *testing.T) {
	f, store, _ := newFlow(t, nil, fixedSessions{hasElicit: false})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskExternalSideEffect, ApprovalTimeout: time.Second}
	out, _ := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x"})
	if out.Decision != "fallback_required" {
		t.Errorf("decision = %q, want fallback_required", out.Decision)
	}
	if !out.FallbackRequired() {
		t.Errorf("FallbackRequired should be true")
	}
	pending, _ := store.ListPending(context.Background(), "acme")
	if len(pending) != 1 {
		t.Errorf("fallback should leave a pending row; got %d", len(pending))
	}
}

func TestFlow_ResolveManually_AllowsLater(t *testing.T) {
	f, store, em := newFlow(t, nil, fixedSessions{hasElicit: false})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskExternalSideEffect, ApprovalTimeout: time.Minute}
	out, _ := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x"})
	if !out.FallbackRequired() {
		t.Fatal("expected fallback_required")
	}
	res, err := f.ResolveManually(context.Background(), "acme", out.Approval.ID, approval.StatusApproved, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != approval.StatusApproved {
		t.Errorf("manual resolve did not mark approved: %+v", res)
	}
	// Store reflects the change.
	got, _ := store.Get(context.Background(), "acme", out.Approval.ID)
	if got.Status != approval.StatusApproved {
		t.Errorf("store status = %q, want approved", got.Status)
	}
	// Audit got a decided event with channel=manual.
	events := em.Events()
	last := events[len(events)-1]
	if last.Type != audit.EventApprovalDecided {
		t.Errorf("last event type = %q", last.Type)
	}
	if last.Payload["channel"] != "manual" {
		t.Errorf("channel = %v", last.Payload["channel"])
	}
}

func TestFlow_Sweep_ExpiresPendings(t *testing.T) {
	f, store, _ := newFlow(t, nil, fixedSessions{hasElicit: false})
	dec := policy.Decision{Tool: "github.x", RiskClass: policy.RiskDestructive, ApprovalTimeout: time.Nanosecond}
	out, _ := f.Run(context.Background(), "acme", "s1", "u1", dec, approval.CallContext{Tool: "github.x"})
	time.Sleep(10 * time.Millisecond)
	n, err := f.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("sweep should expire at least one row; got %d", n)
	}
	got, _ := store.Get(context.Background(), "acme", out.Approval.ID)
	if got.Status != approval.StatusExpired {
		t.Errorf("expected expired after sweep; got %q", got.Status)
	}
}
