package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// approvalDTO is the JSON shape returned by /v1/approvals*. We avoid
// returning the internal *approval.Approval directly so we can drop
// fields that may carry sensitive metadata.
type approvalDTO struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	SessionID   string         `json:"session_id"`
	UserID      string         `json:"user_id,omitempty"`
	Tool        string         `json:"tool"`
	ArgsSummary string         `json:"args_summary,omitempty"`
	RiskClass   string         `json:"risk_class"`
	Status      string         `json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	DecidedAt   *time.Time     `json:"decided_at,omitempty"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func toApprovalDTO(a *approval.Approval) approvalDTO {
	if a == nil {
		return approvalDTO{}
	}
	return approvalDTO{
		ID:          a.ID,
		TenantID:    a.TenantID,
		SessionID:   a.SessionID,
		UserID:      a.UserID,
		Tool:        a.Tool,
		ArgsSummary: a.ArgsSummary,
		RiskClass:   a.RiskClass,
		Status:      a.Status,
		CreatedAt:   a.CreatedAt,
		DecidedAt:   a.DecidedAt,
		ExpiresAt:   a.ExpiresAt,
		Metadata:    a.Metadata,
	}
}

func listApprovalsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.Approvals == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "approvals_not_configured", "approval flow disabled", nil)
			return
		}
		// Default behavior: list pending approvals. Other status filters
		// land in a follow-up; explicitly reject them so silent fallback
		// to "pending" doesn't hide a misuse.
		if s := r.URL.Query().Get("status"); s != "" && s != "pending" {
			writeJSONError(w, http.StatusBadRequest, "unsupported_filter", "only status=pending is currently supported", nil)
			return
		}
		records, err := d.Approvals.ListPending(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]approvalDTO, 0, len(records))
		for _, rec := range records {
			out = append(out, toApprovalDTO(recordToApproval(rec)))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func getApprovalHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.Approvals == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "approvals_not_configured", "approval flow disabled", nil)
			return
		}
		rec, err := d.Approvals.Get(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "approval not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "lookup_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toApprovalDTO(recordToApproval(rec)))
	}
}

func resolveApprovalHandler(d Deps, status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.ApprovalFlow == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "approvals_not_configured", "approval flow disabled", nil)
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		if r.ContentLength > 0 {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		ap, err := d.ApprovalFlow.ResolveManually(r.Context(), id.TenantID, chi.URLParam(r, "id"), status, id.UserID)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "approval not found", nil)
				return
			}
			writeJSONError(w, http.StatusBadRequest, "resolve_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toApprovalDTO(ap))
	}
}

// recordToApproval converts the storage record to the approval flow's
// in-memory shape. Mirrors approval.NewStorageAdapter's recordToApproval
// without leaking the unexported helper.
func recordToApproval(r *ifaces.ApprovalRecord) *approval.Approval {
	if r == nil {
		return nil
	}
	a := &approval.Approval{
		ID:          r.ID,
		TenantID:    r.TenantID,
		SessionID:   r.SessionID,
		UserID:      r.UserID,
		Tool:        r.Tool,
		ArgsSummary: r.ArgsSummary,
		RiskClass:   r.RiskClass,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt,
		DecidedAt:   r.DecidedAt,
		ExpiresAt:   r.ExpiresAt,
	}
	if r.MetadataJSON != "" {
		_ = json.Unmarshal([]byte(r.MetadataJSON), &a.Metadata)
	}
	return a
}
