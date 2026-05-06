package api

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// listResourcesHandler implements GET /v1/resources. Tenant-scoped via
// the auth middleware. Cursor is pulled from ?cursor=... and round-trips
// through the aggregator.
func listResourcesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agg := d.Dispatcher.Resources()
		if agg == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "resources_unavailable", "resource aggregator not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		sess := &mcpgw.Session{
			ID:       "rest:" + id.TenantID,
			TenantID: id.TenantID,
		}
		res, err := agg.ListAll(r.Context(), sess, r.URL.Query().Get("cursor"))
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "resources_list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// readResourceHandler implements GET /v1/resources/{uri}. The URI is
// expected to be url-encoded once on the wire (chi decodes the path
// param so the handler sees the canonical mcp+server:// form).
func readResourceHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agg := d.Dispatcher.Resources()
		if agg == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "resources_unavailable", "resource aggregator not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		uri := chi.URLParam(r, "*")
		if uri == "" {
			uri = chi.URLParam(r, "uri")
		}
		if decoded, err := url.QueryUnescape(uri); err == nil {
			uri = decoded
		}
		sess := &mcpgw.Session{ID: "rest:" + id.TenantID, TenantID: id.TenantID}
		res, err := agg.Read(r.Context(), sess, uri)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "resources_read_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// listResourceTemplatesHandler implements GET /v1/resources/templates.
func listResourceTemplatesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agg := d.Dispatcher.Resources()
		if agg == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "resources_unavailable", "resource aggregator not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		sess := &mcpgw.Session{ID: "rest:" + id.TenantID, TenantID: id.TenantID}
		res, err := agg.ListTemplates(r.Context(), sess, r.URL.Query().Get("cursor"))
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "templates_list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
