package budgets

import (
	"context"
	"fmt"
	"time"
)

// LevelStatus is the live state of one budget within the scope hierarchy: how
// much of its current window is used vs the limit, when it resets, and the
// remaining-headroom percentage (0–100, clamped).
type LevelStatus struct {
	Level       string  `json:"level"`  // vk|team|customer|tenant
	Metric      string  `json:"metric"` // requests|tokens|cost_usd
	BudgetID    string  `json:"budget_id"`
	Period      string  `json:"period"`
	Used        float64 `json:"used"`
	Limit       float64 `json:"limit"`
	ResetsAt    string  `json:"resets_at"`
	HeadroomPct float64 `json:"headroom_pct"`
}

// Headroom returns the live status of every enabled budget across the scope
// chain (most-specific first), for the budget read API + the Console headroom
// bars. `now` is explicit for determinism.
func (e *Enforcer) Headroom(ctx context.Context, tenantID string, scopes []Scope, now time.Time) ([]LevelStatus, error) {
	var out []LevelStatus
	for _, sc := range scopes {
		bs, err := e.store.ListBudgetsByScope(ctx, tenantID, sc.Kind, sc.ID)
		if err != nil {
			return nil, fmt.Errorf("budgets: headroom list %s/%s: %w", sc.Kind, sc.ID, err)
		}
		for _, b := range bs {
			win, err := WindowFor(now, Period(b.Period), Alignment(b.Alignment))
			if err != nil {
				return nil, fmt.Errorf("budgets: headroom window %s: %w", b.ID, err)
			}
			used, err := e.usedFor(ctx, tenantID, b.ID, win.Key)
			if err != nil {
				return nil, err
			}
			out = append(out, LevelStatus{
				Level:       sc.Kind,
				Metric:      b.Metric,
				BudgetID:    b.ID,
				Period:      b.Period,
				Used:        used,
				Limit:       b.LimitVal,
				ResetsAt:    win.ResetsAt.UTC().Format(time.RFC3339),
				HeadroomPct: headroomPct(used, b.LimitVal),
			})
		}
	}
	return out, nil
}

// headroomPct is the remaining percentage of a budget (100 = untouched, 0 = at
// or over limit), clamped to [0,100].
func headroomPct(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	pct := (1 - used/limit) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
