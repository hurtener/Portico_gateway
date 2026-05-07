package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// playgroundCorrelationHandler GET /api/playground/sessions/{sid}/correlation
// returns the correlated bundle (spans + audits + policy + drift). The
// `?since=` query param filters incrementally.
func playgroundCorrelationHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		sid := chi.URLParam(r, "sid")
		var since time.Time
		if s := r.URL.Query().Get("since"); s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				since = t
			}
		}
		bundle, err := d.Playground.Correlation(r.Context(), sid, since)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "correlation_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	}
}

// runCorrelationHandler GET /api/playground/runs/{run_id}/correlation
// returns the bundle bound to the run's session id.
func runCorrelationHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		runID := chi.URLParam(r, "run_id")
		bundle, err := d.Playground.RunCorrelation(r.Context(), runID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "correlation_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	}
}
