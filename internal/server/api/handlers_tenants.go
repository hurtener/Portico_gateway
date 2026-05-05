package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// listTenantsHandler GET /v1/admin/tenants
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

// getTenantHandler GET /v1/admin/tenants/{id}
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

// upsertTenantHandler POST /v1/admin/tenants
func upsertTenantHandler(d Deps) http.HandlerFunc {
	type body struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Plan        string `json:"plan"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var b body
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if err := validateTenantBody(b.ID, b.DisplayName, b.Plan); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error(), nil)
			return
		}
		_, getErr := d.Tenants.Get(r.Context(), b.ID)
		isCreate := IsErrNotFound(getErr)

		t := &ifaces.Tenant{
			ID:          b.ID,
			DisplayName: b.DisplayName,
			Plan:        b.Plan,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := d.Tenants.Upsert(r.Context(), t); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "upsert_failed", err.Error(), nil)
			return
		}
		// Re-fetch to capture canonical timestamps
		stored, _ := d.Tenants.Get(r.Context(), b.ID)
		status := http.StatusOK
		if isCreate {
			status = http.StatusCreated
		}
		writeJSON(w, status, stored)
	}
}

func validateTenantBody(id, name, plan string) error {
	if id == "" {
		return errors.New("id is required")
	}
	if name == "" {
		return errors.New("display_name is required")
	}
	if plan == "" {
		return errors.New("plan is required")
	}
	return nil
}
