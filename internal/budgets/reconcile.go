package budgets

import (
	"context"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Warning is emitted when a budget ledger crosses an 80/95/100% threshold for
// the first time within its current window (debounced via the ledger's
// last_warning_level). At 100% the budget is also enforcing (the next pre-check
// denies); the warning lets operators react before that.
type Warning struct {
	Level    string  `json:"level"`
	Metric   string  `json:"metric"`
	BudgetID string  `json:"budget_id"`
	Pct      int     `json:"pct"` // 80 | 95 | 100
	Used     float64 `json:"used"`
	Limit    float64 `json:"limit"`
}

// warnThresholds are the crossing points, highest first.
var warnThresholds = []int{100, 95, 80}

// Reconcile applies the actual usage of a completed call to EVERY applicable
// budget across the scope hierarchy, atomically (one transaction — a fault on
// any level rolls back all of them). It then returns any newly-crossed
// 80/95/100% warnings, recording each so it fires only once per window.
func (e *Enforcer) Reconcile(ctx context.Context, tenantID string, scopes []Scope, now time.Time, actual Usage) ([]Warning, error) {
	type touched struct {
		budget    *ifaces.Budget
		level     string
		windowKey string
	}
	var (
		updates []ifaces.LedgerUpdate
		seen    []touched
	)
	for _, sc := range scopes {
		budgets, err := e.store.ListBudgetsByScope(ctx, tenantID, sc.Kind, sc.ID)
		if err != nil {
			return nil, fmt.Errorf("budgets: reconcile list %s/%s: %w", sc.Kind, sc.ID, err)
		}
		for _, b := range budgets {
			amt := actual.metric(b.Metric)
			if amt <= 0 {
				continue
			}
			win, err := WindowFor(now, Period(b.Period), Alignment(b.Alignment))
			if err != nil {
				return nil, fmt.Errorf("budgets: reconcile window for %s: %w", b.ID, err)
			}
			updates = append(updates, ifaces.LedgerUpdate{
				BudgetID:  b.ID,
				WindowKey: win.Key,
				Delta:     amt,
				ResetsAt:  win.ResetsAt.UTC().Format(time.RFC3339),
			})
			seen = append(seen, touched{budget: b, level: sc.Kind, windowKey: win.Key})
		}
	}
	if len(updates) == 0 {
		return nil, nil
	}
	if err := e.store.ReconcileUsage(ctx, tenantID, updates); err != nil {
		return nil, fmt.Errorf("budgets: reconcile usage: %w", err)
	}

	// Warnings: re-read each touched ledger (now updated) and fire the highest
	// newly-crossed threshold, debounced via last_warning_level.
	var warnings []Warning
	for _, t := range seen {
		le, ok, err := e.store.GetLedger(ctx, tenantID, t.budget.ID, t.windowKey)
		if err != nil {
			return warnings, fmt.Errorf("budgets: reconcile reread ledger %s: %w", t.budget.ID, err)
		}
		if !ok || t.budget.LimitVal <= 0 {
			continue
		}
		pct := int(le.Used / t.budget.LimitVal * 100)
		crossed := highestCrossed(pct)
		if crossed == 0 || crossed <= le.LastWarningLevel {
			continue // below 80%, or already warned at this level this window
		}
		if err := e.store.SetLedgerWarningLevel(ctx, tenantID, t.budget.ID, t.windowKey, crossed); err != nil {
			return warnings, fmt.Errorf("budgets: set warning level %s: %w", t.budget.ID, err)
		}
		warnings = append(warnings, Warning{
			Level:    t.level,
			Metric:   t.budget.Metric,
			BudgetID: t.budget.ID,
			Pct:      crossed,
			Used:     le.Used,
			Limit:    t.budget.LimitVal,
		})
	}
	return warnings, nil
}

// highestCrossed returns the highest warning threshold pct has reached (0 when
// below 80%).
func highestCrossed(pct int) int {
	for _, th := range warnThresholds {
		if pct >= th {
			return th
		}
	}
	return 0
}
