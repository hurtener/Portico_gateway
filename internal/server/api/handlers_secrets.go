package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/secrets"
)

// VaultManager is the API-facing surface of the secrets vault. Declared
// here so the api package doesn't import the concrete vault driver.
type VaultManager interface {
	Get(ctx context.Context, tenantID, name string) (string, error)
	Put(ctx context.Context, tenantID, name, value string) error
	Delete(ctx context.Context, tenantID, name string) error
	List(ctx context.Context, tenantID string) ([]string, error)
}

func listAdminSecretsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		// Admin secrets endpoint enumerates across every tenant — but we
		// only emit (tenant, name) pairs, never values.
		all := []map[string]string{}
		// Iterate over every tenant the operator has registered. We
		// reuse the tenant store for the list.
		ctx := r.Context()
		tenants, err := d.Tenants.List(ctx)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "tenant_list_failed", err.Error(), nil)
			return
		}
		for _, t := range tenants {
			names, err := d.Vault.List(ctx, t.ID)
			if err != nil {
				continue
			}
			for _, n := range names {
				all = append(all, map[string]string{"tenant_id": t.ID, "name": n})
			}
		}
		writeJSON(w, http.StatusOK, all)
	}
}

func putAdminSecretHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		tenantID := chi.URLParam(r, "tenant")
		name := chi.URLParam(r, "name")
		if tenantID == "" || name == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_path", "tenant and name required", nil)
			return
		}
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", err.Error(), nil)
			return
		}
		if body.Value == "" {
			writeJSONError(w, http.StatusBadRequest, "empty_value", "value required", nil)
			return
		}
		if err := d.Vault.Put(r.Context(), tenantID, name, body.Value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "put_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func deleteAdminSecretHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		tenantID := chi.URLParam(r, "tenant")
		name := chi.URLParam(r, "name")
		if err := d.Vault.Delete(r.Context(), tenantID, name); err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "secret not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
