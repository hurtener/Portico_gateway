package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// listPromptsHandler implements GET /v1/prompts.
func listPromptsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agg := d.Dispatcher.Prompts()
		if agg == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "prompts_unavailable", "prompt aggregator not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		sess := &mcpgw.Session{ID: "rest:" + id.TenantID, TenantID: id.TenantID}
		res, err := agg.ListAll(r.Context(), sess, r.URL.Query().Get("cursor"))
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "prompts_list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// getPromptHandler implements POST /v1/prompts/{name}. Arguments arrive
// in the JSON body so the URL stays simple. The qualified name is in
// the path.
func getPromptHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agg := d.Dispatcher.Prompts()
		if agg == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "prompts_unavailable", "prompt aggregator not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		name := chi.URLParam(r, "name")

		var body struct {
			Arguments map[string]string `json:"arguments"`
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

		sess := &mcpgw.Session{ID: "rest:" + id.TenantID, TenantID: id.TenantID}
		res, err := agg.Get(r.Context(), sess, name, body.Arguments)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "prompt_get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
