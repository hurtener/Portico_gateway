package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// activityHandler returns a handler that reads the entity_activity
// projection for the given kind. Mounted as
// /api/{kind}/{id}/activity by the router (kind is captured by the
// closure; id comes from the URL).
func activityHandler(d Deps, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.EntityActivity == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		id, _ := tenant.From(r.Context())
		entityID := chi.URLParam(r, "id")
		if entityID == "" {
			entityID = chi.URLParam(r, "name")
		}
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		rows, err := d.EntityActivity.List(r.Context(), id.TenantID, kind, entityID, limit)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		// Render as a uniform shape — never null.
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			diff := map[string]any{}
			if len(row.DiffJSON) > 0 {
				diff["raw"] = string(row.DiffJSON)
			}
			out = append(out, map[string]any{
				"event_id":      row.EventID,
				"occurred_at":   row.OccurredAt,
				"actor_user_id": row.ActorUserID,
				"summary":       row.Summary,
				"diff":          diff,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}
