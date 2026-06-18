package budgets

import (
	"context"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Metric identifiers (mirror the storage CHECK constraint).
const (
	MetricRequests = "requests"
	MetricTokens   = "tokens"
	MetricCostUSD  = "cost_usd"
)

// Scope is one level of the budget hierarchy. Callers pass scopes most-specific
// first (vk → team → customer → tenant) so the enforcer reports the lowest
// (most-specific) level that fires.
type Scope struct {
	Kind string // vk|team|customer|tenant
	ID   string
}

// Usage is the amount a call consumes — an estimate for the pre-check, or the
// actual measured amount for the post-call reconcile.
type Usage struct {
	Requests float64
	Tokens   float64
	CostUSD  float64
}

// metric returns the component matching a budget's metric.
func (u Usage) metric(m string) float64 {
	switch m {
	case MetricRequests:
		return u.Requests
	case MetricTokens:
		return u.Tokens
	case MetricCostUSD:
		return u.CostUSD
	default:
		return 0
	}
}

// Violation reports the most-specific budget that a call would exceed.
type Violation struct {
	Level    string  `json:"level"`  // scope kind (vk|team|customer|tenant)
	Metric   string  `json:"metric"` // requests|tokens|cost_usd
	BudgetID string  `json:"budget_id"`
	Limit    float64 `json:"limit"`
	Used     float64 `json:"used"`
	Would    float64 `json:"would"` // used + amount this call would add
}

// Enforcer evaluates hierarchical budgets against the ledger. It is pure logic
// over the BudgetStore + window math; it never calls time.Now() itself (the
// caller passes `now` so the behaviour is deterministic + testable).
type Enforcer struct {
	store ifaces.BudgetStore
}

// NewEnforcer builds an enforcer over the budget store.
func NewEnforcer(store ifaces.BudgetStore) *Enforcer { return &Enforcer{store: store} }

// PreCheck walks scopes most-specific → least. For each enabled budget whose
// metric this call consumes, it computes the budget's current window, reads the
// ledger, and reports the FIRST (most-specific) level where used+amount exceeds
// the limit. Returns (nil,nil) when every level has headroom. amount is the
// estimated usage for the in-flight call.
func (e *Enforcer) PreCheck(ctx context.Context, tenantID string, scopes []Scope, now time.Time, amount Usage) (*Violation, error) {
	for _, sc := range scopes {
		budgets, err := e.store.ListBudgetsByScope(ctx, tenantID, sc.Kind, sc.ID)
		if err != nil {
			return nil, fmt.Errorf("budgets: precheck list %s/%s: %w", sc.Kind, sc.ID, err)
		}
		for _, b := range budgets {
			amt := amount.metric(b.Metric)
			if amt <= 0 {
				continue // this call doesn't consume this budget's metric
			}
			win, err := WindowFor(now, Period(b.Period), Alignment(b.Alignment))
			if err != nil {
				return nil, fmt.Errorf("budgets: precheck window for %s: %w", b.ID, err)
			}
			used, err := e.usedFor(ctx, tenantID, b.ID, win.Key)
			if err != nil {
				return nil, err
			}
			if used+amt > b.LimitVal {
				return &Violation{
					Level:    sc.Kind,
					Metric:   b.Metric,
					BudgetID: b.ID,
					Limit:    b.LimitVal,
					Used:     used,
					Would:    used + amt,
				}, nil
			}
		}
	}
	return nil, nil
}

// usedFor returns the current used amount for a budget's window (0 when no
// ledger row exists yet).
func (e *Enforcer) usedFor(ctx context.Context, tenantID, budgetID, windowKey string) (float64, error) {
	le, ok, err := e.store.GetLedger(ctx, tenantID, budgetID, windowKey)
	if err != nil {
		return 0, fmt.Errorf("budgets: get ledger %s/%s: %w", budgetID, windowKey, err)
	}
	if !ok {
		return 0, nil
	}
	return le.Used, nil
}
