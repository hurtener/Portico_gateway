package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// AgentProfileDTO is the JSON representation for REST API.
type AgentProfileDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	AllowedMCPServers   []string `json:"allowed_mcp_servers"`
	AllowedTools        []string `json:"allowed_tools"`
	AllowedSkills       []string `json:"allowed_skills"`
	AllowedModelAliases []string `json:"allowed_model_aliases"`
	Scopes              []string `json:"scopes"`
	PolicyBundleRef     string   `json:"policy_bundle_ref,omitempty"`
	ParentProfileID     string   `json:"parent_profile_id,omitempty"`
	Enabled             bool     `json:"enabled"`
	CreatedAt           string   `json:"created_at,omitempty"`
	UpdatedAt           string   `json:"updated_at,omitempty"`
}

// toAgentProfileDTO converts an ifaces.AgentProfile to AgentProfileDTO.
func toAgentProfileDTO(p *ifaces.AgentProfile) AgentProfileDTO {
	return AgentProfileDTO{
		ID:                  p.ID,
		Name:                p.Name,
		Description:         p.Description,
		AllowedMCPServers:   p.AllowedMCPServers,
		AllowedTools:        p.AllowedTools,
		AllowedSkills:       p.AllowedSkills,
		AllowedModelAliases: p.AllowedModelAliases,
		Scopes:              p.Scopes,
		PolicyBundleRef:     p.PolicyBundleRef,
		ParentProfileID:     p.ParentProfileID,
		Enabled:             p.Enabled,
		CreatedAt:           p.CreatedAt,
		UpdatedAt:           p.UpdatedAt,
	}
}

// requireAgentProfileAdmin gates agent profile CRUD behind admin scope.
func requireAgentProfileAdmin(w http.ResponseWriter, id tenant.Identity) bool {
	if scope.Has(id, "admin") {
		return true
	}
	writeJSONError(w, http.StatusForbidden, "forbidden", "admin scope required", nil)
	return false
}

// randHex16 generates a 16-byte random hex string.
func randHex16() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// listAgentProfilesHandler handles GET /api/agent-profiles.
func listAgentProfilesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.AgentProfiles == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent_profiles_not_configured", "agent profile store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireAgentProfileAdmin(w, id) {
			return
		}
		profiles, err := d.AgentProfiles.List(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]AgentProfileDTO, 0, len(profiles))
		for _, p := range profiles {
			out = append(out, toAgentProfileDTO(p))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getAgentProfileHandler handles GET /api/agent-profiles/{id}.
func getAgentProfileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.AgentProfiles == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent_profiles_not_configured", "agent profile store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireAgentProfileAdmin(w, id) {
			return
		}
		profileID := chi.URLParam(r, "id")
		p, err := d.AgentProfiles.Get(r.Context(), id.TenantID, profileID)
		if err != nil {
			if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "agent profile not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toAgentProfileDTO(p))
	}
}

// createAgentProfileHandler handles POST /api/agent-profiles.
func createAgentProfileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.AgentProfiles == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent_profiles_not_configured", "agent profile store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireAgentProfileAdmin(w, id) {
			return
		}
		var dto AgentProfileDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if dto.Name == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "name is required", nil)
			return
		}
		// Generate ID server-side, ignore any client-supplied ID.
		profileID := "ap_" + randHex16()
		p := &ifaces.AgentProfile{
			TenantID:            id.TenantID,
			ID:                  profileID,
			Name:                dto.Name,
			Description:         dto.Description,
			AllowedMCPServers:   dto.AllowedMCPServers,
			AllowedTools:        dto.AllowedTools,
			AllowedSkills:       dto.AllowedSkills,
			AllowedModelAliases: dto.AllowedModelAliases,
			Scopes:              dto.Scopes,
			PolicyBundleRef:     dto.PolicyBundleRef,
			ParentProfileID:     dto.ParentProfileID,
			Enabled:             dto.Enabled,
		}
		if err := d.AgentProfiles.Put(r.Context(), p); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		// Fetch back to get timestamps populated.
		created, err := d.AgentProfiles.Get(r.Context(), id.TenantID, profileID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_create_failed", err.Error(), nil)
			return
		}
		invalidateProfile(d, id.TenantID, profileID)
		writeJSON(w, http.StatusCreated, toAgentProfileDTO(created))
	}
}

// invalidateProfile drops the resolver's cached entries for the tenant after a
// CRUD write so a profile change takes effect immediately (the 60s TTL is the
// backstop). No-op when no resolver is wired.
func invalidateProfile(d Deps, tenantID, profileID string) {
	if d.ProfileResolver != nil {
		d.ProfileResolver.Invalidate(tenantID, profileID)
	}
}

// updateAgentProfileHandler handles PUT /api/agent-profiles/{id}.
func updateAgentProfileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.AgentProfiles == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent_profiles_not_configured", "agent profile store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireAgentProfileAdmin(w, id) {
			return
		}
		profileID := chi.URLParam(r, "id")
		// Verify it exists first.
		_, err := d.AgentProfiles.Get(r.Context(), id.TenantID, profileID)
		if err != nil {
			if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "agent profile not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		var dto AgentProfileDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		// Use URL id as authoritative, ignore body id.
		p := &ifaces.AgentProfile{
			TenantID:            id.TenantID,
			ID:                  profileID,
			Name:                dto.Name,
			Description:         dto.Description,
			AllowedMCPServers:   dto.AllowedMCPServers,
			AllowedTools:        dto.AllowedTools,
			AllowedSkills:       dto.AllowedSkills,
			AllowedModelAliases: dto.AllowedModelAliases,
			Scopes:              dto.Scopes,
			PolicyBundleRef:     dto.PolicyBundleRef,
			ParentProfileID:     dto.ParentProfileID,
			Enabled:             dto.Enabled,
		}
		if err := d.AgentProfiles.Put(r.Context(), p); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
			return
		}
		// Fetch back to get updated timestamps.
		updated, err := d.AgentProfiles.Get(r.Context(), id.TenantID, profileID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_update_failed", err.Error(), nil)
			return
		}
		invalidateProfile(d, id.TenantID, profileID)
		writeJSON(w, http.StatusOK, toAgentProfileDTO(updated))
	}
}

// deleteAgentProfileHandler handles DELETE /api/agent-profiles/{id}.
func deleteAgentProfileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.AgentProfiles == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "agent_profiles_not_configured", "agent profile store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireAgentProfileAdmin(w, id) {
			return
		}
		profileID := chi.URLParam(r, "id")
		err := d.AgentProfiles.Delete(r.Context(), id.TenantID, profileID)
		if err != nil {
			if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "agent profile not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		invalidateProfile(d, id.TenantID, profileID)
		w.WriteHeader(http.StatusNoContent)
	}
}
