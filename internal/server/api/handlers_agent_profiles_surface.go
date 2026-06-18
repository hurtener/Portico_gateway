package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/profiles"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// agentProfileSurfaceHandler handles GET /api/agent-profiles/{id}/surface
// (acceptance #12). It returns the profile's allowlists intersected with the
// LIVE catalog — registered servers, loaded skills, configured model aliases —
// so an operator can see exactly what the profile resolves to right now. The
// live read is the point: a server registered after the profile was created but
// matching its allowlist shows up here immediately.
func agentProfileSurfaceHandler(d Deps) http.HandlerFunc {
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
		ap, err := d.AgentProfiles.Get(r.Context(), id.TenantID, profileID)
		if err != nil {
			if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "agent profile not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		prof := profiles.FromStore(ap)
		live := profiles.LiveCatalog{
			Servers: liveServerIDs(r.Context(), d, id.TenantID),
			Skills:  liveSkillIDs(d),
			Aliases: liveModelAliases(r.Context(), d, id.TenantID),
		}
		writeJSON(w, http.StatusOK, prof.Materialize(live))
	}
}

// liveServerIDs returns the ids of every MCP server currently registered to the
// tenant.
func liveServerIDs(ctx context.Context, d Deps, tenantID string) []string {
	if d.Registry == nil {
		return nil
	}
	snaps, err := d.Registry.List(ctx, tenantID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, s.Spec.ID)
	}
	return out
}

// liveSkillIDs returns the ids of every loaded Skill Pack in the catalog.
func liveSkillIDs(d Deps) []string {
	mgr := skillsMgr(d)
	if mgr == nil || mgr.Catalog() == nil {
		return nil
	}
	skills := mgr.Catalog().List()
	out := make([]string, 0, len(skills))
	seen := make(map[string]struct{}, len(skills))
	for _, s := range skills {
		if s == nil || s.Manifest == nil {
			continue
		}
		if _, dup := seen[s.Manifest.ID]; dup {
			continue
		}
		seen[s.Manifest.ID] = struct{}{}
		out = append(out, s.Manifest.ID)
	}
	return out
}

// liveModelAliases returns the configured LLM model aliases for the tenant.
func liveModelAliases(ctx context.Context, d Deps, tenantID string) []string {
	if d.LLMModels == nil {
		return nil
	}
	models, err := d.LLMModels.ListModels(ctx, tenantID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.Alias)
	}
	return out
}
