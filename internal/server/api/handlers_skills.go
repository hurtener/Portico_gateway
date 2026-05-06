package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/skills/runtime"
)

// skillsManager is the slim contract handlers depend on. The concrete
// runtime.Manager satisfies it; using an interface keeps this file
// from importing the whole runtime package unnecessarily.
type skillsManager interface {
	Catalog() *runtime.Catalog
	Enablement() *runtime.Enablement
	IndexGenerator() *runtime.IndexGenerator
}

// listSkillsHandler implements GET /v1/skills.
func listSkillsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mgr := skillsMgr(d)
		if mgr == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skills_unavailable", "skills runtime not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		body, err := mgr.IndexGenerator().Render(r.Context(), id.TenantID, "")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "skills_render_failed", err.Error(), nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// getSkillHandler implements GET /v1/skills/{id}.
func getSkillHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mgr := skillsMgr(d)
		if mgr == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skills_unavailable", "skills runtime not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		s, ok := mgr.Catalog().Get(skillID)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "not_found", "unknown skill", map[string]any{"skill_id": skillID})
			return
		}
		enabledTenant, _ := mgr.Enablement().IsEnabled(r.Context(), id.TenantID, "", skillID)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                 s.Manifest.ID,
			"version":            s.Manifest.Version,
			"title":              s.Manifest.Title,
			"description":        s.Manifest.Description,
			"manifest":           s.Manifest,
			"warnings":           s.Warnings,
			"enabled_for_tenant": enabledTenant,
		})
	}
}

// getSkillManifestYAML implements GET /v1/skills/{id}/manifest.yaml.
func getSkillManifestYAML(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mgr := skillsMgr(d)
		if mgr == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skills_unavailable", "skills runtime not configured", nil)
			return
		}
		_ = tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		s, ok := mgr.Catalog().Get(skillID)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "not_found", "unknown skill", map[string]any{"skill_id": skillID})
			return
		}
		body, err := yaml.Marshal(s.Manifest)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "yaml_marshal_failed", err.Error(), nil)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// enableSkillHandler implements POST /v1/skills/{id}/enable (tenant-wide).
func enableSkillHandler(d Deps, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mgr := skillsMgr(d)
		if mgr == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skills_unavailable", "skills runtime not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		if _, ok := mgr.Catalog().Get(skillID); !ok {
			writeJSONError(w, http.StatusNotFound, "not_found", "unknown skill", map[string]any{"skill_id": skillID})
			return
		}
		if err := mgr.Enablement().Set(r.Context(), id.TenantID, "", skillID, enabled); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "enablement_set_failed", err.Error(), nil)
			return
		}
		mgr.IndexGenerator().Invalidate(id.TenantID, "")
		writeJSON(w, http.StatusOK, map[string]any{
			"skill_id": skillID,
			"enabled":  enabled,
			"scope":    "tenant",
		})
	}
}

// listSessionSkillsHandler implements GET /v1/sessions/{session_id}/skills.
func listSessionSkillsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mgr := skillsMgr(d)
		if mgr == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skills_unavailable", "skills runtime not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		sessionID := chi.URLParam(r, "session_id")
		rules, err := mgr.Enablement().ListForSession(r.Context(), id.TenantID, sessionID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "enablement_list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rules})
	}
}

// sessionSkillEnableHandler implements POST /v1/sessions/{session_id}/skills/enable
// (and the `disable` mirror with enabled=false).
func sessionSkillEnableHandler(d Deps, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mgr := skillsMgr(d)
		if mgr == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skills_unavailable", "skills runtime not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		sessionID := chi.URLParam(r, "session_id")
		if sessionID == "" {
			writeJSONError(w, http.StatusBadRequest, "bad_path", "session_id required", nil)
			return
		}
		var body struct {
			SkillID string `json:"skill_id"`
		}
		if r.Body != nil {
			defer r.Body.Close()
			raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
				return
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
					return
				}
			}
		}
		if body.SkillID == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_skill_id", "skill_id is required in the body", nil)
			return
		}
		if _, ok := mgr.Catalog().Get(body.SkillID); !ok {
			writeJSONError(w, http.StatusNotFound, "not_found", "unknown skill", map[string]any{"skill_id": body.SkillID})
			return
		}
		if err := mgr.Enablement().Set(r.Context(), id.TenantID, sessionID, body.SkillID, enabled); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "enablement_set_failed", err.Error(), nil)
			return
		}
		mgr.IndexGenerator().Invalidate(id.TenantID, sessionID)
		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID,
			"skill_id":   body.SkillID,
			"enabled":    enabled,
			"scope":      "session",
		})
	}
}

func skillsMgr(d Deps) skillsManager {
	if d.Skills == nil {
		return nil
	}
	return d.Skills
}
