package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// listTenantsHandler GET /v1/admin/tenants and GET /api/admin/tenants.
func listTenantsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ts, err := d.Tenants.List(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		if ts == nil {
			ts = []*ifaces.Tenant{}
		}
		writeJSON(w, http.StatusOK, ts)
	}
}

// getTenantHandler GET /v1/admin/tenants/{id} and GET /api/admin/tenants/{id}.
func getTenantHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		t, err := d.Tenants.Get(r.Context(), id)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "tenant not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, t)
	}
}

// upsertTenantHandler POST /v1/admin/tenants and PUT /api/admin/tenants/{id}.
//
// POST creates (returns 201 on new id, 200 on existing — back-compat with
// Phase 0). PUT replaces (always 200 on success, 404 if missing).
func upsertTenantHandler(d Deps, isPut bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b tenantWriteBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if isPut {
			pathID := chi.URLParam(r, "id")
			if b.ID == "" {
				b.ID = pathID
			} else if b.ID != pathID {
				writeJSONError(w, http.StatusBadRequest, "id_mismatch", "path id and body id must match",
					map[string]any{"path": pathID, "body": b.ID})
				return
			}
		}
		if err := validateTenantBody(b); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
			return
		}

		_, getErr := d.Tenants.Get(r.Context(), b.ID)
		isCreate := IsErrNotFound(getErr)
		if isPut && isCreate {
			writeJSONError(w, http.StatusNotFound, "not_found", "tenant not found", map[string]any{"id": b.ID})
			return
		}

		t := b.toTenant()
		if err := d.Tenants.Upsert(r.Context(), t); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "upsert_failed", err.Error(), nil)
			return
		}
		stored, _ := d.Tenants.Get(r.Context(), b.ID)
		status := http.StatusOK
		if isCreate {
			status = http.StatusCreated
		}
		emitTenantEvent(d, r, b.ID, isCreate)
		writeJSON(w, status, stored)
	}
}

// deleteTenantHandler DELETE /api/admin/tenants/{id} — archives the
// tenant (status='archived'). Hard delete is /purge.
func deleteTenantHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		t, err := d.Tenants.Get(r.Context(), id)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "tenant not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		t.Status = "archived"
		t.UpdatedAt = time.Now().UTC()
		if err := d.Tenants.Upsert(r.Context(), t); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "archive_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, "tenant.archived", "tenant", id, "archived",
			map[string]any{"tenant_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

// purgeTenantHandler POST /api/admin/tenants/{id}/purge — destructive,
// admin scope + approval-gate required (gate is wired in router).
func purgeTenantHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := d.Tenants.Delete(r.Context(), id); err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "tenant not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "purge_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, "tenant.purged", "tenant", id, "purged",
			map[string]any{"tenant_id": id})
		w.WriteHeader(http.StatusNoContent)
	}
}

// tenantWriteBody is the JSON shape POST/PUT accepts. New Phase 9 fields
// default at the storage layer, so callers can omit them safely.
type tenantWriteBody struct {
	ID                    string `json:"id"`
	DisplayName           string `json:"display_name"`
	Plan                  string `json:"plan"`
	RuntimeMode           string `json:"runtime_mode,omitempty"`
	MaxConcurrentSessions int    `json:"max_concurrent_sessions,omitempty"`
	MaxRequestsPerMinute  int    `json:"max_requests_per_minute,omitempty"`
	AuditRetentionDays    int    `json:"audit_retention_days,omitempty"`
	JWTIssuer             string `json:"jwt_issuer,omitempty"`
	JWTJWKSURL            string `json:"jwt_jwks_url,omitempty"`
	Status                string `json:"status,omitempty"`
}

func (b tenantWriteBody) toTenant() *ifaces.Tenant {
	return &ifaces.Tenant{
		ID:                    b.ID,
		DisplayName:           b.DisplayName,
		Plan:                  b.Plan,
		RuntimeMode:           b.RuntimeMode,
		MaxConcurrentSessions: b.MaxConcurrentSessions,
		MaxRequestsPerMinute:  b.MaxRequestsPerMinute,
		AuditRetentionDays:    b.AuditRetentionDays,
		JWTIssuer:             b.JWTIssuer,
		JWTJWKSURL:            b.JWTJWKSURL,
		Status:                b.Status,
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}
}

func validateTenantBody(b tenantWriteBody) error {
	if b.ID == "" {
		return errors.New("id is required")
	}
	if b.DisplayName == "" {
		return errors.New("display_name is required")
	}
	if b.Plan == "" {
		return errors.New("plan is required")
	}
	if b.Status != "" && b.Status != "active" && b.Status != "archived" {
		return errors.New("status must be 'active' or 'archived'")
	}
	return nil
}
