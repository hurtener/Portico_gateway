package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// replayPlaygroundCaseHandler POST /api/playground/cases/{id}/replay
// re-issues the saved case and records a Run row.
func replayPlaygroundCaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		caseID := chi.URLParam(r, "id")
		run, err := d.Playground.Replay(r.Context(), id.TenantID, id.UserID, caseID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "replay_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	}
}
