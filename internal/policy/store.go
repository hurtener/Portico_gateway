package policy

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// RuleStore is the SQL-backed surface for editor / engine.
//
// The tests for the editor + dryrun stay free of SQL — they exercise pure
// in-memory rulesets. RuleStore wraps the storage iface and adds a
// Watch() channel so the engine + Console can react to mutations without
// polling.
type RuleStore struct {
	store ifaces.PolicyRulesStore

	subMu       sync.RWMutex
	subscribers map[chan RuleChange]struct{}
}

// RuleChange describes a single mutation. Tenant is always present;
// RuleID is empty on bulk-replace events. Op is one of "upsert", "delete",
// "replace".
type RuleChange struct {
	Tenant string
	RuleID string
	Op     string
	At     time.Time
}

// NewRuleStore wraps a storage-level PolicyRulesStore.
func NewRuleStore(s ifaces.PolicyRulesStore) *RuleStore {
	if s == nil {
		return nil
	}
	return &RuleStore{
		store:       s,
		subscribers: make(map[chan RuleChange]struct{}),
	}
}

// List returns the tenant's ruleset in priority order.
func (rs *RuleStore) List(ctx context.Context, tenantID string) (RuleSet, error) {
	if rs == nil || rs.store == nil {
		return RuleSet{}, errors.New("policy: rule store not configured")
	}
	rows, err := rs.store.List(ctx, tenantID)
	if err != nil {
		return RuleSet{}, err
	}
	out := RuleSet{Rules: make([]Rule, 0, len(rows))}
	for _, row := range rows {
		conds, err := DecodeConditions(row.Conditions)
		if err != nil {
			return RuleSet{}, err
		}
		actions, err := DecodeActions(row.Actions)
		if err != nil {
			return RuleSet{}, err
		}
		out.Rules = append(out.Rules, Rule{
			ID:         row.RuleID,
			Priority:   row.Priority,
			Enabled:    row.Enabled,
			RiskClass:  row.RiskClass,
			Conditions: conds,
			Actions:    actions,
			Notes:      row.Notes,
			UpdatedAt:  row.UpdatedAt,
			UpdatedBy:  row.UpdatedBy,
		})
	}
	return out, nil
}

// Get returns one rule.
func (rs *RuleStore) Get(ctx context.Context, tenantID, ruleID string) (Rule, error) {
	if rs == nil || rs.store == nil {
		return Rule{}, errors.New("policy: rule store not configured")
	}
	row, err := rs.store.Get(ctx, tenantID, ruleID)
	if err != nil {
		return Rule{}, err
	}
	conds, err := DecodeConditions(row.Conditions)
	if err != nil {
		return Rule{}, err
	}
	actions, err := DecodeActions(row.Actions)
	if err != nil {
		return Rule{}, err
	}
	return Rule{
		ID:         row.RuleID,
		Priority:   row.Priority,
		Enabled:    row.Enabled,
		RiskClass:  row.RiskClass,
		Conditions: conds,
		Actions:    actions,
		Notes:      row.Notes,
		UpdatedAt:  row.UpdatedAt,
		UpdatedBy:  row.UpdatedBy,
	}, nil
}

// Upsert validates and writes one rule.
func (rs *RuleStore) Upsert(ctx context.Context, tenantID string, r Rule) (Rule, error) {
	if rs == nil || rs.store == nil {
		return Rule{}, errors.New("policy: rule store not configured")
	}
	if err := Validate(r); err != nil {
		return Rule{}, err
	}
	conds, err := EncodeConditions(r.Conditions)
	if err != nil {
		return Rule{}, err
	}
	actions, err := EncodeActions(r.Actions)
	if err != nil {
		return Rule{}, err
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	row := &ifaces.PolicyRuleRecord{
		TenantID:   tenantID,
		RuleID:     r.ID,
		Priority:   r.Priority,
		Enabled:    r.Enabled,
		RiskClass:  r.RiskClass,
		Conditions: conds,
		Actions:    actions,
		Notes:      r.Notes,
		UpdatedAt:  r.UpdatedAt,
		UpdatedBy:  r.UpdatedBy,
	}
	if err := rs.store.Upsert(ctx, row); err != nil {
		return Rule{}, err
	}
	rs.publish(RuleChange{Tenant: tenantID, RuleID: r.ID, Op: "upsert", At: time.Now().UTC()})
	return r, nil
}

// Delete removes a rule.
func (rs *RuleStore) Delete(ctx context.Context, tenantID, ruleID string) error {
	if rs == nil || rs.store == nil {
		return errors.New("policy: rule store not configured")
	}
	if err := rs.store.Delete(ctx, tenantID, ruleID); err != nil {
		return err
	}
	rs.publish(RuleChange{Tenant: tenantID, RuleID: ruleID, Op: "delete", At: time.Now().UTC()})
	return nil
}

// ReplaceAll atomically swaps the tenant's ruleset.
func (rs *RuleStore) ReplaceAll(ctx context.Context, tenantID string, set RuleSet) error {
	if rs == nil || rs.store == nil {
		return errors.New("policy: rule store not configured")
	}
	rows := make([]*ifaces.PolicyRuleRecord, 0, len(set.Rules))
	for _, r := range set.Rules {
		if err := Validate(r); err != nil {
			return err
		}
		conds, err := EncodeConditions(r.Conditions)
		if err != nil {
			return err
		}
		actions, err := EncodeActions(r.Actions)
		if err != nil {
			return err
		}
		updated := r.UpdatedAt
		if updated.IsZero() {
			updated = time.Now().UTC()
		}
		rows = append(rows, &ifaces.PolicyRuleRecord{
			TenantID:   tenantID,
			RuleID:     r.ID,
			Priority:   r.Priority,
			Enabled:    r.Enabled,
			RiskClass:  r.RiskClass,
			Conditions: conds,
			Actions:    actions,
			Notes:      r.Notes,
			UpdatedAt:  updated,
			UpdatedBy:  r.UpdatedBy,
		})
	}
	if err := rs.store.ReplaceAll(ctx, tenantID, rows); err != nil {
		return err
	}
	rs.publish(RuleChange{Tenant: tenantID, Op: "replace", At: time.Now().UTC()})
	return nil
}

// Watch returns a channel that receives every mutation. Buffered (16);
// drops oldest on overflow. Unsubscribe via Unwatch.
func (rs *RuleStore) Watch() <-chan RuleChange {
	if rs == nil {
		ch := make(chan RuleChange)
		close(ch)
		return ch
	}
	ch := make(chan RuleChange, 16)
	rs.subMu.Lock()
	rs.subscribers[ch] = struct{}{}
	rs.subMu.Unlock()
	return ch
}

// Unwatch removes the subscriber and closes the channel.
func (rs *RuleStore) Unwatch(ch <-chan RuleChange) {
	if rs == nil {
		return
	}
	rs.subMu.Lock()
	defer rs.subMu.Unlock()
	for c := range rs.subscribers {
		if c == ch {
			delete(rs.subscribers, c)
			close(c)
			return
		}
	}
}

func (rs *RuleStore) publish(ev RuleChange) {
	rs.subMu.RLock()
	subs := make([]chan RuleChange, 0, len(rs.subscribers))
	for c := range rs.subscribers {
		subs = append(subs, c)
	}
	rs.subMu.RUnlock()
	for _, c := range subs {
		select {
		case c <- ev:
		default:
			select {
			case <-c:
			default:
			}
			select {
			case c <- ev:
			default:
			}
		}
	}
}
