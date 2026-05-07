// Phase 8: REST handlers for /api/skill-sources. Requires JWT scope
// "skills:read" (list/get) or "skills:write" (mutating). Tenant scope
// is read from tenant.MustFrom(ctx).

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// SkillSourcesController is the slim contract the API depends on.
// The concrete impl is wired in cmd/portico from the SQLite store +
// the per-tenant Source registry.
type SkillSourcesController interface {
	List(ctx context.Context, tenantID string) ([]*ifaces.SkillSourceRecord, error)
	Get(ctx context.Context, tenantID, name string) (*ifaces.SkillSourceRecord, error)
	Upsert(ctx context.Context, rec *ifaces.SkillSourceRecord) error
	Delete(ctx context.Context, tenantID, name string) error
	Refresh(ctx context.Context, tenantID, name string) error
	ListPacks(ctx context.Context, tenantID, name string) ([]SourcePack, error)
}

// SourcePack is one entry in /api/skill-sources/{name}/packs.
type SourcePack struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Loc     string `json:"loc,omitempty"`
}

// listSkillSourcesHandler implements GET /api/skill-sources.
func listSkillSourcesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.SkillSources
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skill_sources_unavailable", "skill source registry not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		rows, err := c.List(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": skillSourceRecordsToDTO(rows)})
	}
}

func getSkillSourceHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.SkillSources
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skill_sources_unavailable", "skill source registry not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		name := chi.URLParam(r, "name")
		rec, err := c.Get(r.Context(), id.TenantID, name)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "skill source not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, skillSourceToDTO(rec))
	}
}

func upsertSkillSourceHandler(d Deps, isUpdate bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.SkillSources
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skill_sources_unavailable", "skill source registry not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())

		var body skillSourceDTO
		if r.Body != nil {
			defer r.Body.Close()
			raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
				return
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
				return
			}
		}
		if isUpdate {
			body.Name = chi.URLParam(r, "name")
		}
		if body.Name == "" || body.Driver == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_fields", "name and driver are required", nil)
			return
		}
		cfgJSON, err := json.Marshal(body.Config)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_config", err.Error(), nil)
			return
		}
		rec := &ifaces.SkillSourceRecord{
			TenantID:       id.TenantID,
			Name:           body.Name,
			Driver:         body.Driver,
			ConfigJSON:     cfgJSON,
			CredentialRef:  body.CredentialRef,
			RefreshSeconds: body.RefreshSeconds,
			Priority:       body.Priority,
			Enabled:        body.Enabled,
		}
		if !body.EnabledSet {
			rec.Enabled = true
		}
		if rec.RefreshSeconds == 0 {
			rec.RefreshSeconds = 300
		}
		if rec.Priority == 0 {
			rec.Priority = 100
		}
		if err := c.Upsert(r.Context(), rec); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "upsert_failed", err.Error(), nil)
			return
		}
		// Re-fetch to surface the persisted timestamps.
		fresh, err := c.Get(r.Context(), id.TenantID, body.Name)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "post_upsert_get_failed", err.Error(), nil)
			return
		}
		status := http.StatusCreated
		if isUpdate {
			status = http.StatusOK
		}
		writeJSON(w, status, skillSourceToDTO(fresh))
	}
}

func deleteSkillSourceHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.SkillSources
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skill_sources_unavailable", "skill source registry not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		name := chi.URLParam(r, "name")
		if err := c.Delete(r.Context(), id.TenantID, name); err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "skill source not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func refreshSkillSourceHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.SkillSources
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skill_sources_unavailable", "skill source registry not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		name := chi.URLParam(r, "name")
		if err := c.Refresh(r.Context(), id.TenantID, name); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "refresh_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"refreshed": time.Now().UTC().Format(time.RFC3339)})
	}
}

func listSkillSourcePacksHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.SkillSources
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "skill_sources_unavailable", "skill source registry not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		name := chi.URLParam(r, "name")
		packs, err := c.ListPacks(r.Context(), id.TenantID, name)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_packs_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": packs})
	}
}

// --- DTOs -----------------------------------------------------------

type skillSourceDTO struct {
	Name           string                 `json:"name"`
	Driver         string                 `json:"driver"`
	Config         map[string]interface{} `json:"config"`
	CredentialRef  string                 `json:"credential_ref,omitempty"`
	RefreshSeconds int                    `json:"refresh_seconds,omitempty"`
	Priority       int                    `json:"priority,omitempty"`
	Enabled        bool                   `json:"enabled"`
	EnabledSet     bool                   `json:"-"`

	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	LastRefreshAt string `json:"last_refresh_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

func (d *skillSourceDTO) UnmarshalJSON(data []byte) error {
	type alias skillSourceDTO
	tmp := struct {
		alias
		Enabled *bool `json:"enabled"`
	}{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*d = skillSourceDTO(tmp.alias)
	if tmp.Enabled != nil {
		d.EnabledSet = true
		d.Enabled = *tmp.Enabled
	}
	return nil
}

func skillSourceToDTO(rec *ifaces.SkillSourceRecord) skillSourceDTO {
	cfg := map[string]any{}
	_ = json.Unmarshal(rec.ConfigJSON, &cfg)
	out := skillSourceDTO{
		Name:           rec.Name,
		Driver:         rec.Driver,
		Config:         cfg,
		CredentialRef:  rec.CredentialRef,
		RefreshSeconds: rec.RefreshSeconds,
		Priority:       rec.Priority,
		Enabled:        rec.Enabled,
		CreatedAt:      rec.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      rec.UpdatedAt.Format(time.RFC3339),
		LastError:      rec.LastError,
	}
	if rec.LastRefreshAt != nil {
		out.LastRefreshAt = rec.LastRefreshAt.Format(time.RFC3339)
	}
	return out
}

func skillSourceRecordsToDTO(rows []*ifaces.SkillSourceRecord) []skillSourceDTO {
	out := make([]skillSourceDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, skillSourceToDTO(r))
	}
	return out
}
