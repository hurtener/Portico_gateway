package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// BudgetDTO is the JSON view of a governance Budget.
type BudgetDTO struct {
	ID        string  `json:"id"`
	ScopeKind string  `json:"scope_kind"`
	ScopeID   string  `json:"scope_id"`
	Metric    string  `json:"metric"`
	Period    string  `json:"period"`
	Alignment string  `json:"alignment"`
	LimitVal  float64 `json:"limit_val"`
	Enabled   bool    `json:"enabled"`
	CreatedAt string  `json:"created_at,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
}

func toBudgetDTO(b *ifaces.Budget) BudgetDTO {
	return BudgetDTO{
		ID:        b.ID,
		ScopeKind: b.ScopeKind,
		ScopeID:   b.ScopeID,
		Metric:    b.Metric,
		Period:    b.Period,
		Alignment: b.Alignment,
		LimitVal:  b.LimitVal,
		Enabled:   b.Enabled,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

// budgetCreateRequest is the POST body for creating a Budget. scope_kind,
// scope_id, metric, period are required; alignment defaults to "rolling".
type budgetCreateRequest struct {
	ScopeKind string  `json:"scope_kind"`
	ScopeID   string  `json:"scope_id"`
	Metric    string  `json:"metric"`
	Period    string  `json:"period"`
	Alignment string  `json:"alignment"`
	LimitVal  float64 `json:"limit_val"`
	Enabled   bool    `json:"enabled"`
}

// budgetUpdateRequest is the PUT body for updating a Budget.
type budgetUpdateRequest struct {
	ScopeKind string  `json:"scope_kind"`
	ScopeID   string  `json:"scope_id"`
	Metric    string  `json:"metric"`
	Period    string  `json:"period"`
	Alignment string  `json:"alignment"`
	LimitVal  float64 `json:"limit_val"`
	Enabled   bool    `json:"enabled"`
}

// budgetsConfigured gates budget CRUD behind the store being wired.
func budgetsConfigured(d Deps, w http.ResponseWriter) bool {
	if d.Budgets == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "budgets_not_configured", "budget store not configured", nil)
		return false
	}
	return true
}

// listBudgetsHandler: GET /api/governance/budgets.
func listBudgetsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !budgetsConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		budgets, err := d.Budgets.ListBudgets(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]BudgetDTO, 0, len(budgets))
		for _, b := range budgets {
			out = append(out, toBudgetDTO(b))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getBudgetHandler: GET /api/governance/budgets/{id}.
func getBudgetHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !budgetsConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		b, err := d.Budgets.GetBudget(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, ifaces.ErrBudgetNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "budget not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toBudgetDTO(b))
	}
}

// createBudgetHandler: POST /api/governance/budgets. Requires scope_kind,
// scope_id, metric, period; alignment defaults to "rolling" when empty.
// Generates the id server-side ("bdg_"+randHex16()).
func createBudgetHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !budgetsConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		var req budgetCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.ScopeKind == "" || req.ScopeID == "" || req.Metric == "" || req.Period == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "scope_kind, scope_id, metric, period are required", nil)
			return
		}
		alignment := req.Alignment
		if alignment == "" {
			alignment = "rolling"
		}
		budgetID := "bdg_" + randHex16()
		b := &ifaces.Budget{
			TenantID:  id.TenantID,
			ID:        budgetID,
			ScopeKind: req.ScopeKind,
			ScopeID:   req.ScopeID,
			Metric:    req.Metric,
			Period:    req.Period,
			Alignment: alignment,
			LimitVal:  req.LimitVal,
			Enabled:   req.Enabled,
		}
		if err := d.Budgets.PutBudget(r.Context(), b); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		created, err := d.Budgets.GetBudget(r.Context(), id.TenantID, budgetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, toBudgetDTO(created))
	}
}

// updateBudgetHandler: PUT /api/governance/budgets/{id}. Get-then-overwrite-
// then-Put. 404 when the row does not exist.
func updateBudgetHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !budgetsConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		budgetID := chi.URLParam(r, "id")
		existing, err := d.Budgets.GetBudget(r.Context(), id.TenantID, budgetID)
		if err != nil {
			if errors.Is(err, ifaces.ErrBudgetNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "budget not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		var req budgetUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		// Overwrite the mutable fields; preserve id + tenant + created_at.
		if req.ScopeKind != "" {
			existing.ScopeKind = req.ScopeKind
		}
		if req.ScopeID != "" {
			existing.ScopeID = req.ScopeID
		}
		if req.Metric != "" {
			existing.Metric = req.Metric
		}
		if req.Period != "" {
			existing.Period = req.Period
		}
		if req.Alignment != "" {
			existing.Alignment = req.Alignment
		}
		existing.LimitVal = req.LimitVal
		existing.Enabled = req.Enabled
		if err := d.Budgets.PutBudget(r.Context(), existing); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
			return
		}
		updated, err := d.Budgets.GetBudget(r.Context(), id.TenantID, budgetID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_update_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toBudgetDTO(updated))
	}
}

// deleteBudgetHandler: DELETE /api/governance/budgets/{id}. 204 on success.
func deleteBudgetHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !budgetsConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		if err := d.Budgets.DeleteBudget(r.Context(), id.TenantID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, ifaces.ErrBudgetNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "budget not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
