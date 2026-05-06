package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

// snapshotDTO mirrors snapshots.Snapshot for the REST surface. Field
// names are stable; the Console reads them directly via the typed
// api.ts client.
type snapshotDTO struct {
	ID          string                     `json:"id"`
	TenantID    string                     `json:"tenant_id"`
	SessionID   string                     `json:"session_id,omitempty"`
	CreatedAt   time.Time                  `json:"created_at"`
	OverallHash string                     `json:"overall_hash"`
	Servers     []snapshots.ServerInfo     `json:"servers"`
	Tools       []snapshots.ToolInfo       `json:"tools"`
	Resources   []snapshots.ResourceInfo   `json:"resources"`
	Prompts     []snapshots.PromptInfo     `json:"prompts"`
	Skills      []snapshots.SkillInfo      `json:"skills"`
	Policies    snapshots.PoliciesInfo     `json:"policies"`
	Credentials []snapshots.CredentialInfo `json:"credentials"`
	Warnings    []string                   `json:"warnings,omitempty"`
}

func toSnapshotDTO(s *snapshots.Snapshot) snapshotDTO {
	if s == nil {
		return snapshotDTO{}
	}
	return snapshotDTO{
		ID:          s.ID,
		TenantID:    s.TenantID,
		SessionID:   s.SessionID,
		CreatedAt:   s.CreatedAt,
		OverallHash: s.OverallHash,
		Servers:     s.Servers,
		Tools:       s.Tools,
		Resources:   s.Resources,
		Prompts:     s.Prompts,
		Skills:      s.Skills,
		Policies:    s.Policies,
		Credentials: s.Credentials,
		Warnings:    s.Warnings,
	}
}

func listSnapshotsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.Snapshots == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "snapshots_not_configured", "snapshot service disabled", nil)
			return
		}
		q := snapshots.ListQuery{
			Limit:  parseLimit(r.URL.Query().Get("limit"), 50, 200),
			Cursor: r.URL.Query().Get("cursor"),
		}
		if s := r.URL.Query().Get("since"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				q.Since = t
			}
		}
		out, next, err := d.Snapshots.List(r.Context(), id.TenantID, q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		dtos := make([]snapshotDTO, 0, len(out))
		for _, s := range out {
			dtos = append(dtos, toSnapshotDTO(s))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"snapshots":   dtos,
			"next_cursor": next,
		})
	}
}

func getSnapshotHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Snapshots == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "snapshots_not_configured", "snapshot service disabled", nil)
			return
		}
		id := chi.URLParam(r, "id")
		s, err := d.Snapshots.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, snapshots.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "snapshot not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "lookup_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toSnapshotDTO(s))
	}
}

func diffSnapshotsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Snapshots == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "snapshots_not_configured", "snapshot service disabled", nil)
			return
		}
		a := chi.URLParam(r, "a")
		b := chi.URLParam(r, "b")
		diff, err := d.Snapshots.Diff(r.Context(), a, b)
		if err != nil {
			if errors.Is(err, snapshots.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", err.Error(), nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "diff_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, diff)
	}
}

// resolveCatalogHandler creates (or returns the existing) snapshot for
// the supplied session_id. Lets the smoke harness — and clients that
// want to deliberately materialise a snapshot — anchor an audit trail
// without firing a tools/list.
func resolveCatalogHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.Snapshots == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "snapshots_not_configured", "snapshot service disabled", nil)
			return
		}
		var body struct {
			SessionID string `json:"session_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s, err := d.Snapshots.Create(r.Context(), id.TenantID, body.SessionID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toSnapshotDTO(s))
	}
}

func sessionSnapshotHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.SnapshotBinder == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "snapshots_not_configured", "snapshot binder disabled", nil)
			return
		}
		sid := chi.URLParam(r, "session_id")
		s, ok := d.SnapshotBinder.Lookup(sid)
		if !ok || s == nil {
			writeJSONError(w, http.StatusNotFound, "not_found", "no snapshot for session", nil)
			return
		}
		writeJSON(w, http.StatusOK, toSnapshotDTO(s))
	}
}
