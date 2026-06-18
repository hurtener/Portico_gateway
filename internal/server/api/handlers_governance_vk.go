package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// VirtualKeyDTO is the JSON view of a Virtual Key. It NEVER includes the secret
// (only Create/Rotate return the one-time token, in a separate field).
type VirtualKeyDTO struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	ParentKind         string   `json:"parent_kind"`
	ParentID           string   `json:"parent_id,omitempty"`
	ProfileID          string   `json:"profile_id,omitempty"`
	Scopes             []string `json:"scopes"`
	ProviderAllowlist  []string `json:"provider_allowlist"`
	ModelAllowlist     []string `json:"model_allowlist"`
	MCPServerAllowlist []string `json:"mcp_server_allowlist"`
	Enabled            bool     `json:"enabled"`
	CreatedAt          string   `json:"created_at,omitempty"`
	RotatedAt          string   `json:"rotated_at,omitempty"`
	RevokedAt          string   `json:"revoked_at,omitempty"`
}

func toVirtualKeyDTO(vk *ifaces.VirtualKey) VirtualKeyDTO {
	return VirtualKeyDTO{
		ID:                 vk.ID,
		Name:               vk.Name,
		ParentKind:         vk.ParentKind,
		ParentID:           vk.ParentID,
		ProfileID:          vk.ProfileID,
		Scopes:             vk.Scopes,
		ProviderAllowlist:  vk.ProviderAllowlist,
		ModelAllowlist:     vk.ModelAllowlist,
		MCPServerAllowlist: vk.MCPServerAllowlist,
		Enabled:            vk.Enabled,
		CreatedAt:          vk.CreatedAt,
		RotatedAt:          vk.RotatedAt,
		RevokedAt:          vk.RevokedAt,
	}
}

// vkCreateRequest is the POST body for creating a Virtual Key.
type vkCreateRequest struct {
	Name               string   `json:"name"`
	Scopes             []string `json:"scopes"`
	ProviderAllowlist  []string `json:"provider_allowlist"`
	ModelAllowlist     []string `json:"model_allowlist"`
	MCPServerAllowlist []string `json:"mcp_server_allowlist"`
	ProfileID          string   `json:"profile_id"`
	ParentKind         string   `json:"parent_kind"`
	ParentID           string   `json:"parent_id"`
}

// vkCreatedResponse returns the new VK plus the one-time secret token.
type vkCreatedResponse struct {
	VirtualKey VirtualKeyDTO `json:"virtual_key"`
	Token      string        `json:"token"` // pk-portico-… — shown once, never retrievable again.
}

// requireGovernanceAdmin gates governance CRUD behind admin scope.
func requireGovernanceAdmin(w http.ResponseWriter, id tenant.Identity) bool {
	if scope.Has(id, "admin") {
		return true
	}
	writeJSONError(w, http.StatusForbidden, "forbidden", "admin scope required", nil)
	return false
}

func vkConfigured(d Deps, w http.ResponseWriter) bool {
	if d.VKService == nil || d.Governance == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "governance_not_configured", "virtual keys not configured", nil)
		return false
	}
	return true
}

// listVirtualKeysHandler: GET /api/governance/virtual-keys.
func listVirtualKeysHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !vkConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		vks, err := d.Governance.ListVirtualKeys(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]VirtualKeyDTO, 0, len(vks))
		for _, vk := range vks {
			out = append(out, toVirtualKeyDTO(vk))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getVirtualKeyHandler: GET /api/governance/virtual-keys/{id}. Never returns the secret.
func getVirtualKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !vkConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		vk, err := d.Governance.GetVirtualKey(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "virtual key not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toVirtualKeyDTO(vk))
	}
}

// createVirtualKeyHandler: POST /api/governance/virtual-keys. Returns the secret once.
func createVirtualKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !vkConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		var req vkCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Name == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "name is required", nil)
			return
		}
		created, err := d.VKService.Create(r.Context(), virtualkeys.CreateParams{
			TenantID:           id.TenantID,
			Name:               req.Name,
			Scopes:             req.Scopes,
			ProviderAllowlist:  req.ProviderAllowlist,
			ModelAllowlist:     req.ModelAllowlist,
			MCPServerAllowlist: req.MCPServerAllowlist,
			ProfileID:          req.ProfileID,
			ParentKind:         req.ParentKind,
			ParentID:           req.ParentID,
		})
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, vkCreatedResponse{
			VirtualKey: toVirtualKeyDTO(created.VK),
			Token:      created.Token,
		})
	}
}

// rotateVirtualKeyHandler: POST /api/governance/virtual-keys/{id}/rotate. Returns the new secret once.
func rotateVirtualKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !vkConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		vkID := chi.URLParam(r, "id")
		rotated, err := d.VKService.Rotate(r.Context(), id.TenantID, vkID)
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "virtual key not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "rotate_failed", err.Error(), nil)
			return
		}
		invalidateVK(d, vkID)
		writeJSON(w, http.StatusOK, vkCreatedResponse{
			VirtualKey: toVirtualKeyDTO(rotated.VK),
			Token:      rotated.Token,
		})
	}
}

// deleteVirtualKeyHandler: DELETE /api/governance/virtual-keys/{id} (revoke).
func deleteVirtualKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !vkConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		vkID := chi.URLParam(r, "id")
		if err := d.VKService.Revoke(r.Context(), id.TenantID, vkID); err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "virtual key not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "revoke_failed", err.Error(), nil)
			return
		}
		invalidateVK(d, vkID)
		w.WriteHeader(http.StatusNoContent)
	}
}

// invalidateVK drops the resolver's cached entries for a VK so a rotate/revoke
// takes effect immediately (the resolver TTL is the backstop). No-op when no
// resolver is wired.
func invalidateVK(d Deps, vkID string) {
	if d.VKResolver != nil {
		d.VKResolver.InvalidateVK(vkID)
	}
}
