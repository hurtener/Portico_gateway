package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
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

// VaultRevealManager is the slim surface the REST layer needs from the
// reveal manager (Phase 9). cmd/portico wires a *secrets.RevealManager.
type VaultRevealManager interface {
	IssueRevealToken(ctx context.Context, tenant, name, actorID string) (secrets.RevealToken, error)
	ConsumeReveal(ctx context.Context, token string) (plaintext, tenant, name, actor string, err error)
}

// listAdminSecretsHandler GET /v1/admin/secrets — back-compat. Lists
// (tenant, name) pairs across every registered tenant.
func listAdminSecretsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		ctx := r.Context()
		tenants, err := d.Tenants.List(ctx)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "tenant_list_failed", err.Error(), nil)
			return
		}
		all := []map[string]string{}
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

// putAdminSecretHandler PUT /v1/admin/secrets/{tenant}/{name} — Phase 5
// flat path retained.
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
		emitEntityEvent(d, r, audit.EventSecretUpdated, "secret", tenantID+"/"+name, "secret.updated",
			map[string]any{"tenant_id": tenantID, "name": name})
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteAdminSecretHandler DELETE /v1/admin/secrets/{tenant}/{name}.
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
		emitEntityEvent(d, r, audit.EventSecretDeleted, "secret", tenantID+"/"+name, "secret.deleted",
			map[string]any{"tenant_id": tenantID, "name": name})
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---- Phase 9 endpoints under /api/admin/secrets/* -----------------

// listAPISecretsHandler GET /api/admin/secrets — returns the requesting
// tenant's secrets. Admin scope can pass ?tenant=X to query another tenant.
func listAPISecretsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		tenantID := id.TenantID
		if q := r.URL.Query().Get("tenant"); q != "" && id.HasScope("admin") {
			tenantID = q
		}
		names, err := d.Vault.List(r.Context(), tenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]map[string]any, 0, len(names))
		for _, n := range names {
			out = append(out, map[string]any{
				"tenant_id":  tenantID,
				"name":       n,
				"version":    1,
				"updated_at": "",
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// createAPISecretHandler POST /api/admin/secrets — body: {tenant, name, value}.
func createAPISecretHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		var body struct {
			Tenant string `json:"tenant_id"`
			Name   string `json:"name"`
			Value  string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		id, _ := tenant.From(r.Context())
		if body.Tenant == "" {
			body.Tenant = id.TenantID
		}
		if body.Tenant != id.TenantID && !id.HasScope("admin") {
			writeJSONError(w, http.StatusForbidden, "permission_denied", "cross-tenant requires admin scope", nil)
			return
		}
		if body.Name == "" || body.Value == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "name and value required", nil)
			return
		}
		if err := d.Vault.Put(r.Context(), body.Tenant, body.Name, body.Value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "put_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventSecretCreated, "secret", body.Tenant+"/"+body.Name, "secret.created",
			map[string]any{"tenant_id": body.Tenant, "name": body.Name})
		writeJSON(w, http.StatusCreated, map[string]any{
			"tenant_id":  body.Tenant,
			"name":       body.Name,
			"version":    1,
			"created_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// getAPISecretMetadataHandler GET /api/admin/secrets/{name} — metadata only.
func getAPISecretMetadataHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		name := chi.URLParam(r, "name")
		// Confirm existence by listing.
		names, err := d.Vault.List(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		found := false
		for _, n := range names {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			writeJSONError(w, http.StatusNotFound, "not_found", "secret not found", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id": id.TenantID,
			"name":      name,
			"version":   1,
		})
	}
}

// putAPISecretHandler PUT /api/admin/secrets/{name} — update.
func putAPISecretHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		name := chi.URLParam(r, "name")
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if body.Value == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "value required", nil)
			return
		}
		if err := d.Vault.Put(r.Context(), id.TenantID, name, body.Value); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "put_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventSecretUpdated, "secret", id.TenantID+"/"+name, "secret.updated",
			map[string]any{"tenant_id": id.TenantID, "name": name})
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id":  id.TenantID,
			"name":       name,
			"version":    2,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// deleteAPISecretHandler DELETE /api/admin/secrets/{name}.
func deleteAPISecretHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		name := chi.URLParam(r, "name")
		if err := d.Vault.Delete(r.Context(), id.TenantID, name); err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "secret not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventSecretDeleted, "secret", id.TenantID+"/"+name, "secret.deleted",
			map[string]any{"tenant_id": id.TenantID, "name": name})
		w.WriteHeader(http.StatusNoContent)
	}
}

// rotateAPISecretHandler POST /api/admin/secrets/{name}/rotate — re-encrypts
// the entry under the current root key. Effectively a Get+Put cycle.
func rotateAPISecretHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Vault == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "vault_not_configured", "set PORTICO_VAULT_KEY", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		name := chi.URLParam(r, "name")
		pt, err := d.Vault.Get(r.Context(), id.TenantID, name)
		if err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "secret not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		if err := d.Vault.Put(r.Context(), id.TenantID, name, pt); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "rotate_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventSecretRotated, "secret", id.TenantID+"/"+name, "secret.rotated",
			map[string]any{"tenant_id": id.TenantID, "name": name})
		writeJSON(w, http.StatusOK, map[string]any{"status": "rotated"})
	}
}

// revealIssueHandler POST /api/admin/secrets/{name}/reveal — returns a
// short-lived token. The Console immediately fetches the plaintext via
// reveal/{token} and shows it once.
func revealIssueHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.VaultReveal == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "reveal_not_configured", "reveal manager not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		name := chi.URLParam(r, "name")
		tok, err := d.VaultReveal.IssueRevealToken(r.Context(), id.TenantID, name, id.UserID)
		if err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "secret not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "reveal_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventSecretRevealIssued, "secret", id.TenantID+"/"+name, "secret.reveal.issued",
			map[string]any{"tenant_id": id.TenantID, "name": name})
		writeJSON(w, http.StatusOK, tok)
	}
}

// revealConsumeHandler GET /api/admin/secrets/reveal/{token} — one-shot.
func revealConsumeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.VaultReveal == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "reveal_not_configured", "reveal manager not configured", nil)
			return
		}
		token := chi.URLParam(r, "token")
		pt, tenantID, name, _, err := d.VaultReveal.ConsumeReveal(r.Context(), token)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "reveal_invalid", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventSecretRevealConsumed, "secret", tenantID+"/"+name, "secret.reveal.consumed",
			map[string]any{"tenant_id": tenantID, "name": name})
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id": tenantID,
			"name":      name,
			"value":     pt,
		})
	}
}

// rotateRootHandler POST /api/admin/secrets/rotate-root — re-encrypts every
// entry. Admin + approval-gated. The plan calls for a grace mapping table;
// V1 ships the simpler "atomic rotate or revert" path that the existing
// FileVault.RotateKey already provides.
func rotateRootHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, http.StatusNotImplemented, "rotate_root_unimplemented",
			"rotate-root requires PORTICO_VAULT_KEY_NEXT and is operator-only via the CLI for V1", nil)
	}
}
