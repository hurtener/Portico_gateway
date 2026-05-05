package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// auditQueryHandler implements GET /v1/audit/events.
//
// Phase 0 returns whatever the audit store returns (empty for fresh installs).
// Phase 5 layers redaction + cursor pagination on top of the same handler.
func auditQueryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())

		q := ifaces.AuditQuery{
			TenantID: id.TenantID,
			Limit:    parseLimit(r.URL.Query().Get("limit"), 100, 1000),
			Cursor:   r.URL.Query().Get("cursor"),
		}
		if t := r.URL.Query().Get("type"); t != "" {
			q.Types = strings.Split(t, ",")
		}
		if s := r.URL.Query().Get("since"); s != "" {
			if v, err := time.Parse(time.RFC3339, s); err == nil {
				q.Since = v
			}
		}
		if u := r.URL.Query().Get("until"); u != "" {
			if v, err := time.Parse(time.RFC3339, u); err == nil {
				q.Until = v
			}
		}

		events, next, err := d.Audit.Query(r.Context(), q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "audit_query_failed", err.Error(), nil)
			return
		}
		// Always return an empty array (not null) so clients can iterate without nil checks.
		if events == nil {
			events = []*ifaces.AuditEvent{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"events":      events,
			"next_cursor": next,
		})
	}
}

func parseLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
