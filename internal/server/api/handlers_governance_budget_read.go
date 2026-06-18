package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/budgets"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// virtualKeyBudgetHandler: GET /api/governance/virtual-keys/{id}/budget — the
// live hierarchical headroom for a VK (its own budgets plus its team/customer/
// tenant parents). Used by the Console headroom bars (acceptance #25).
func virtualKeyBudgetHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.BudgetEnforcer == nil || d.Governance == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "budgets_not_configured", "budget engine not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		vkID := chi.URLParam(r, "id")
		vk, err := d.Governance.GetVirtualKey(r.Context(), id.TenantID, vkID)
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "virtual key not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		chain := budgetScopeChain(r.Context(), d, id.TenantID, &virtualkeys.Resolved{
			VKID: vk.ID, TenantID: vk.TenantID, ParentKind: vk.ParentKind, ParentID: vk.ParentID,
		})
		levels, err := d.BudgetEnforcer.Headroom(r.Context(), id.TenantID, chain, time.Now().UTC())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "headroom_failed", err.Error(), nil)
			return
		}
		if levels == nil {
			levels = []budgets.LevelStatus{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"vk_id": vkID, "levels": levels})
	}
}
