package api

import (
	"context"
	"net/http"
	"time"

	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/budgets"
)

// budgetScopeChain builds the hierarchical budget scope chain for a request,
// most-specific first (vk → team → customer → tenant). A VK contributes its own
// level plus its budget parent (a team, whose customer is resolved, or a
// customer directly). Tenant-level budgets always apply, including for non-VK
// (JWT) callers.
func budgetScopeChain(ctx context.Context, d Deps, tenantID string, vk *virtualkeys.Resolved) []budgets.Scope {
	var chain []budgets.Scope
	if vk != nil {
		chain = append(chain, budgets.Scope{Kind: "vk", ID: vk.VKID})
		switch vk.ParentKind {
		case "team":
			chain = append(chain, budgets.Scope{Kind: "team", ID: vk.ParentID})
			if d.Governance != nil {
				if tm, err := d.Governance.GetTeam(ctx, tenantID, vk.ParentID); err == nil && tm.CustomerID != "" {
					chain = append(chain, budgets.Scope{Kind: "customer", ID: tm.CustomerID})
				}
			}
		case "customer":
			chain = append(chain, budgets.Scope{Kind: "customer", ID: vk.ParentID})
		}
	}
	chain = append(chain, budgets.Scope{Kind: "tenant", ID: tenantID})
	return chain
}

// maxTokensOrZero dereferences an optional max_tokens (nil → 0).
func maxTokensOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// unitCostUSD computes the USD cost of an input/output token split via the price
// book (0 when no price book or no entry for the model).
func unitCostUSD(d Deps, ctx context.Context, driver, providerModel string, inputTok, outputTok int) float64 {
	if d.LLMCosts == nil {
		return 0
	}
	uc, err := d.LLMCosts.GetUnitCost(ctx, driver, providerModel)
	if err != nil || uc == nil {
		return 0
	}
	return float64(inputTok)/1000.0*uc.InputPer1K + float64(outputTok)/1000.0*uc.OutputPer1K
}

// checkBudget runs the hierarchical budget pre-check for an in-flight call. It
// returns false (and writes 429 budget_exceeded with the firing level + metric)
// when the call would exceed a budget. A no-op (true) when no enforcer is wired.
// The cost estimate treats maxTokens as worst-case output tokens. Budget-store
// errors fail OPEN (allow + don't block the request on a budget-infra hiccup).
func checkBudget(d Deps, w http.ResponseWriter, r *http.Request, tenantID, driver, providerModel string, maxTokens int) bool {
	if d.BudgetEnforcer == nil {
		return true
	}
	vk, _ := virtualkeys.FromContext(r.Context())
	chain := budgetScopeChain(r.Context(), d, tenantID, vk)
	est := budgets.Usage{
		Requests: 1,
		Tokens:   float64(maxTokens),
		CostUSD:  unitCostUSD(d, r.Context(), driver, providerModel, 0, maxTokens),
	}
	v, err := d.BudgetEnforcer.PreCheck(r.Context(), tenantID, chain, time.Now().UTC(), est)
	if err != nil || v == nil {
		return true
	}
	writeJSONError(w, http.StatusTooManyRequests, "budget_exceeded",
		"budget exceeded at "+v.Level+" level for "+v.Metric,
		map[string]any{"level": v.Level, "metric": v.Metric, "limit": v.Limit, "used": v.Used})
	return false
}

// recordBudgetUsage reconciles the actual usage of a completed call across the
// budget hierarchy (atomic) and emits budget-warning audit events for any newly
// crossed 80/95/100% threshold. Best-effort; never blocks the response.
func recordBudgetUsage(d Deps, r *http.Request, tenantID, driver, providerModel string, inputTok, outputTok int) {
	if d.BudgetEnforcer == nil {
		return
	}
	vk, _ := virtualkeys.FromContext(r.Context())
	chain := budgetScopeChain(r.Context(), d, tenantID, vk)
	actual := budgets.Usage{
		Requests: 1,
		Tokens:   float64(inputTok + outputTok),
		CostUSD:  unitCostUSD(d, r.Context(), driver, providerModel, inputTok, outputTok),
	}
	warnings, err := d.BudgetEnforcer.Reconcile(r.Context(), tenantID, chain, time.Now().UTC(), actual)
	if err != nil {
		return
	}
	for _, wn := range warnings {
		emitBudgetWarning(d, r, tenantID, wn)
	}
}

// emitBudgetWarning audits a budget threshold crossing. 80% → llm.budget_warning;
// 95%/100% → llm.budget_critical (at 100% the budget also enforces). Best-effort.
func emitBudgetWarning(d Deps, r *http.Request, tenantID string, wn budgets.Warning) {
	eventType := "llm.budget_warning"
	if wn.Pct >= 95 {
		eventType = "llm.budget_critical"
	}
	emitWithActor(d, r, eventType, tenantID, map[string]any{
		"level":     wn.Level,
		"metric":    wn.Metric,
		"budget_id": wn.BudgetID,
		"pct":       wn.Pct,
		"used":      wn.Used,
		"limit":     wn.Limit,
	})
}
