package ifaces

import (
	"context"
	"time"
)

// PolicyRuleRecord is the storage row for a single policy rule. Conditions
// and Actions are canonical-JSON encoded so the editor and engine can
// round-trip without semantic drift.
type PolicyRuleRecord struct {
	TenantID   string    `json:"tenant_id"`
	RuleID     string    `json:"rule_id"`
	Priority   int       `json:"priority"`
	Enabled    bool      `json:"enabled"`
	RiskClass  string    `json:"risk_class"`
	Conditions []byte    `json:"-"`
	Actions    []byte    `json:"-"`
	Notes      string    `json:"notes,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  string    `json:"updated_by,omitempty"`
}

// PolicyRulesStore is the SQL-backed surface for tenant_policy_rules.
type PolicyRulesStore interface {
	List(ctx context.Context, tenantID string) ([]*PolicyRuleRecord, error)
	Get(ctx context.Context, tenantID, ruleID string) (*PolicyRuleRecord, error)
	Upsert(ctx context.Context, r *PolicyRuleRecord) error
	Delete(ctx context.Context, tenantID, ruleID string) error
	// ReplaceAll replaces the tenant's full ruleset atomically — every rule
	// not present in `rules` is deleted, every rule present is upserted.
	ReplaceAll(ctx context.Context, tenantID string, rules []*PolicyRuleRecord) error
}
