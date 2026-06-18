package ifaces

import (
	"context"
	"errors"
)

// Budget is a tenant-scoped spend/usage cap on a (scope_kind, scope_id, metric,
// period) tuple. limit_val units depend on metric (requests count, token count,
// or USD). Timestamps are RFC3339 UTC strings.
type Budget struct {
	TenantID  string
	ID        string
	ScopeKind string // vk|team|customer|tenant
	ScopeID   string
	Metric    string // requests|tokens|cost_usd
	Period    string // 1m|1h|1d|1w|1M|1Y
	Alignment string // rolling|calendar
	LimitVal  float64
	Enabled   bool
	CreatedAt string
	UpdatedAt string
}

// LedgerEntry is the accumulated usage for one budget within one time window.
type LedgerEntry struct {
	TenantID         string
	BudgetID         string
	WindowKey        string
	Used             float64
	ResetsAt         string
	LastWarningLevel int // 0/80/95/100 — debounces budget warnings
}

// LedgerUpdate is one atomic increment applied by ReconcileUsage.
type LedgerUpdate struct {
	BudgetID  string
	WindowKey string
	Delta     float64 // added to used
	ResetsAt  string  // set on first insert for this window
}

// ErrBudgetNotFound is returned when no budget matches.
var ErrBudgetNotFound = errors.New("storage: budget not found")

// BudgetStore persists budgets + their ledgers. Tenant-scoped (§6).
type BudgetStore interface {
	PutBudget(ctx context.Context, b *Budget) error
	GetBudget(ctx context.Context, tenantID, id string) (*Budget, error)
	ListBudgets(ctx context.Context, tenantID string) ([]*Budget, error)
	// ListBudgetsByScope returns ENABLED budgets for one scope tuple (used by the
	// pre-call enforcer). scopeKind+scopeID identify the level.
	ListBudgetsByScope(ctx context.Context, tenantID, scopeKind, scopeID string) ([]*Budget, error)
	DeleteBudget(ctx context.Context, tenantID, id string) error

	// GetLedger returns the ledger row for (budget, window); ok=false when absent
	// (NOT an error).
	GetLedger(ctx context.Context, tenantID, budgetID, windowKey string) (*LedgerEntry, bool, error)
	// ReconcileUsage applies ALL updates in ONE transaction (atomic post-call
	// reconcile across budget levels). Each update upserts the ledger row, adding
	// Delta to used. If ANY update fails the whole tx rolls back (no partial).
	ReconcileUsage(ctx context.Context, tenantID string, updates []LedgerUpdate) error
	// SetLedgerWarningLevel records the highest warning level fired for a window
	// (debounce). Upserts the row if absent.
	SetLedgerWarningLevel(ctx context.Context, tenantID, budgetID, windowKey string, level int) error
}
