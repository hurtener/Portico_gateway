package policy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// memRulesStore is an in-memory PolicyRulesStore for tests. It mirrors the
// SQLite store's contract closely enough that the wrapper RuleStore can be
// exercised without a real DB.
type memRulesStore struct {
	mu    sync.Mutex
	rules map[string]*ifaces.PolicyRuleRecord
}

func newMem() *memRulesStore {
	return &memRulesStore{rules: map[string]*ifaces.PolicyRuleRecord{}}
}

func memKey(t, r string) string { return t + "/" + r }

func (m *memRulesStore) List(_ context.Context, tenantID string) ([]*ifaces.PolicyRuleRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*ifaces.PolicyRuleRecord
	for _, r := range m.rules {
		if r.TenantID == tenantID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (m *memRulesStore) Get(_ context.Context, tenantID, ruleID string) (*ifaces.PolicyRuleRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rules[memKey(tenantID, ruleID)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *r
	return &cp, nil
}
func (m *memRulesStore) Upsert(_ context.Context, r *ifaces.PolicyRuleRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.rules[memKey(r.TenantID, r.RuleID)] = &cp
	return nil
}
func (m *memRulesStore) Delete(_ context.Context, tenantID, ruleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rules[memKey(tenantID, ruleID)]; !ok {
		return ifaces.ErrNotFound
	}
	delete(m.rules, memKey(tenantID, ruleID))
	return nil
}
func (m *memRulesStore) ReplaceAll(_ context.Context, tenantID string, rules []*ifaces.PolicyRuleRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rules {
		if r.TenantID == tenantID {
			delete(m.rules, k)
		}
	}
	for _, r := range rules {
		cp := *r
		m.rules[memKey(r.TenantID, r.RuleID)] = &cp
	}
	return nil
}

func TestRuleStore_RoundTrip(t *testing.T) {
	rs := NewRuleStore(newMem())
	ctx := context.Background()
	rule := Rule{
		ID:        "r1",
		Priority:  10,
		Enabled:   true,
		RiskClass: RiskRead,
		Conditions: Conditions{Match: Match{
			Tools: []string{"github.list_repos"},
		}},
		Actions: Actions{Allow: true},
	}
	if _, err := rs.Upsert(ctx, "acme", rule); err != nil {
		t.Fatal(err)
	}
	got, err := rs.Get(ctx, "acme", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.RiskClass != RiskRead {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if len(got.Conditions.Match.Tools) != 1 || got.Conditions.Match.Tools[0] != "github.list_repos" {
		t.Errorf("tools changed: %+v", got.Conditions)
	}
}

func TestRuleStore_ValidateRejectsBadRule(t *testing.T) {
	rs := NewRuleStore(newMem())
	if _, err := rs.Upsert(context.Background(), "acme", Rule{ID: ""}); err == nil {
		t.Errorf("expected validation error")
	}
}

func TestRuleStore_Watch_NotifiesOnUpsert(t *testing.T) {
	rs := NewRuleStore(newMem())
	ch := rs.Watch()
	defer rs.Unwatch(ch)
	go func() {
		_, _ = rs.Upsert(context.Background(), "acme", Rule{
			ID: "r1", RiskClass: RiskRead, Enabled: true, Actions: Actions{Allow: true},
		})
	}()
	select {
	case ev := <-ch:
		if ev.Tenant != "acme" || ev.Op != "upsert" {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestRuleStore_ReplaceAll(t *testing.T) {
	rs := NewRuleStore(newMem())
	ctx := context.Background()
	for i, id := range []string{"a", "b", "c"} {
		if _, err := rs.Upsert(ctx, "acme", Rule{
			ID: id, Priority: i, Enabled: true, RiskClass: RiskRead, Actions: Actions{Allow: true},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := rs.ReplaceAll(ctx, "acme", RuleSet{Rules: []Rule{
		{ID: "d", Priority: 1, Enabled: true, RiskClass: RiskRead, Actions: Actions{Deny: true}},
	}}); err != nil {
		t.Fatal(err)
	}
	got, _ := rs.List(ctx, "acme")
	if len(got.Rules) != 1 || got.Rules[0].ID != "d" {
		t.Errorf("ReplaceAll left stale rules: %+v", got)
	}
}

func TestRuleStore_Delete(t *testing.T) {
	rs := NewRuleStore(newMem())
	ctx := context.Background()
	if _, err := rs.Upsert(ctx, "acme", Rule{
		ID: "r1", Enabled: true, RiskClass: RiskRead, Actions: Actions{Allow: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := rs.Delete(ctx, "acme", "r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Get(ctx, "acme", "r1"); !errors.Is(err, ifaces.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}
