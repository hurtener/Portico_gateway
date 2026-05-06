package api

import (
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// listAppsHandler implements GET /v1/apps. Returns the live MCP Apps
// index. Phase 5 will intersect with policy; Phase 3 returns everything
// the gateway has discovered (the Console is operator-facing).
func listAppsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Apps == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "apps_unavailable", "apps registry not configured", nil)
			return
		}
		_ = tenant.MustFrom(r.Context()) // ensure auth populated; tenant filter Phase 5
		writeJSON(w, http.StatusOK, map[string]any{
			"items": d.Apps.List(),
		})
	}
}
