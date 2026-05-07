// Package middleware hosts api-layer chi middleware factories that don't
// fit neatly inside a single handler file. Phase 10 introduces the
// approval gate for destructive verbs.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ApprovalStore is the slim seam the gate writes through. Mirrors the
// subset of ifaces.ApprovalStore the gate actually uses, so production
// can pass the storage iface directly while tests pass a fake.
type ApprovalStore interface {
	Insert(ctx context.Context, a *ifaces.ApprovalRecord) error
	Get(ctx context.Context, tenantID, id string) (*ifaces.ApprovalRecord, error)
}

// AuditEmitter mirrors api.AuditEmitter to avoid an import cycle.
type AuditEmitter interface {
	Emit(ctx context.Context, e audit.Event)
}

// Config wires the middleware to the surrounding runtime objects. The
// gate is a *guard* — it does not run the destructive action itself; on
// approval, the operator (or the Console) re-issues the original request
// with the X-Approval-Token header set, and the gate lets it through.
type Config struct {
	Store   ApprovalStore
	Audit   AuditEmitter
	Verb    string        // human-readable verb name; e.g. "tenant.delete"
	Timeout time.Duration // approval timeout (default 1h)
}

// approvalRequiredPayload is the JSON body the gate emits on a 202.
type approvalRequiredPayload struct {
	Status            string `json:"status"`
	ApprovalRequestID string `json:"approval_request_id"`
	Verb              string `json:"verb"`
	Message           string `json:"message"`
}

// HeaderApprovalToken is the header the operator sets on their re-issue
// to skip the gate.
const HeaderApprovalToken = "X-Approval-Token"

// NewApprovalGate returns a chi-compatible middleware. When the request
// arrives without an approved-token header, the middleware:
//
//  1. inserts a pending approval row for the (tenant, verb) tuple,
//  2. emits an audit `approval.pending` event,
//  3. returns 202 Accepted with `{approval_request_id}`.
//
// When the request carries `X-Approval-Token: <id>`, the middleware reads
// the row and lets the request through if and only if the row's status is
// `approved`. Mismatched status, expired row, or unknown id all 403.
//
// The gate is opt-in: mount it on specific destructive routes (DELETE
// tenants, secrets rotate-root, etc.) — never as a router-wide wrapper.
func NewApprovalGate(cfg Config) func(http.Handler) http.Handler {
	if cfg.Timeout <= 0 {
		cfg.Timeout = time.Hour
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Store == nil {
				// Mis-wired gate; surface a 503 so operators see it.
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error":   "approval_gate_unavailable",
					"message": "approval store not configured",
				})
				return
			}
			id, _ := tenant.From(r.Context())
			tenantID := id.TenantID
			actor := id.UserID

			if token := r.Header.Get(HeaderApprovalToken); token != "" {
				row, err := cfg.Store.Get(r.Context(), tenantID, token)
				if err != nil || row == nil {
					writeJSON(w, http.StatusForbidden, map[string]any{
						"error":   "approval_token_invalid",
						"message": "approval token not found for this tenant",
					})
					return
				}
				if row.Status != approval.StatusApproved {
					writeJSON(w, http.StatusForbidden, map[string]any{
						"error":   "approval_not_granted",
						"message": "approval token is not in approved state",
						"status":  row.Status,
					})
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// No header — issue a pending approval row and 202.
			now := time.Now().UTC()
			metaBytes, _ := json.Marshal(map[string]any{
				"verb":   cfg.Verb,
				"path":   r.URL.Path,
				"method": r.Method,
				"actor":  actor,
			})
			a := &ifaces.ApprovalRecord{
				ID:           newApprovalID(),
				TenantID:     tenantID,
				UserID:       actor,
				Tool:         cfg.Verb,
				ArgsSummary:  snapshotRequest(r),
				RiskClass:    "destructive",
				Status:       approval.StatusPending,
				CreatedAt:    now,
				ExpiresAt:    now.Add(cfg.Timeout),
				MetadataJSON: string(metaBytes),
			}
			if err := cfg.Store.Insert(r.Context(), a); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":   "approval_insert_failed",
					"message": err.Error(),
				})
				return
			}
			if cfg.Audit != nil {
				cfg.Audit.Emit(r.Context(), audit.Event{
					Type:       audit.EventApprovalPending,
					TenantID:   tenantID,
					UserID:     actor,
					OccurredAt: now,
					Payload: map[string]any{
						"approval_id": a.ID,
						"verb":        cfg.Verb,
						"path":        r.URL.Path,
						"method":      r.Method,
					},
				})
			}
			writeJSON(w, http.StatusAccepted, approvalRequiredPayload{
				Status:            "approval_required",
				ApprovalRequestID: a.ID,
				Verb:              cfg.Verb,
				Message:           "destructive verb requires operator approval",
			})
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// snapshotRequest captures a short, redaction-safe summary of the
// request for the audit row. Method + path; never the body (which is
// frequently a destructive payload).
func snapshotRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Method + " " + r.URL.Path
}

func newApprovalID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "appr_" + time.Now().UTC().Format("20060102T150405.000")
	}
	return "appr_" + hex.EncodeToString(buf[:])
}
