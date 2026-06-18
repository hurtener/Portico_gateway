package api

import (
	"encoding/json"
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	cacheifaces "github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// cacheConfigDTO is the read view of the active cache configuration.
type cacheConfigDTO struct {
	Driver     string  `json:"driver"`
	Scope      string  `json:"scope"`
	TTLSeconds float64 `json:"ttl_seconds"`
	Threshold  float32 `json:"threshold"`
	Enabled    bool    `json:"enabled"`
}

// getCacheConfigHandler: GET /api/llm/cache/config — the active cache settings.
func getCacheConfigHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dto := cacheConfigDTO{
			Scope:      string(d.CacheScope),
			TTLSeconds: d.CacheTTL.Seconds(),
			Threshold:  d.CacheThreshold,
		}
		if d.Cache != nil {
			dto.Driver = d.Cache.Name()
			dto.Enabled = d.Cache.Name() != "none"
		} else {
			dto.Driver = "none"
		}
		writeJSON(w, http.StatusOK, dto)
	}
}

// getCacheStatsHandler: GET /api/llm/cache/stats — per-tenant cache counters.
func getCacheStatsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Cache == nil {
			writeJSON(w, http.StatusOK, cacheifaces.Stats{Driver: "none"})
			return
		}
		id := tenant.MustFrom(r.Context())
		st, err := d.Cache.Stats(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "stats_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}

// invalidateCacheRequest selects which entries to drop. Exactly one of
// alias/scope_id/all is honoured (all wins). Tenant is always the caller's.
type invalidateCacheRequest struct {
	Alias   string `json:"alias"`
	ScopeID string `json:"scope_id"`
	All     bool   `json:"all"`
}

// invalidateCacheHandler: POST /api/llm/cache/invalidate — admin-scoped. Drops
// matching entries for the caller's tenant and audits llm.cache_invalidated.
func invalidateCacheHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Cache == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "cache_not_configured", "no cache configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !scope.Has(id, "admin") {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin scope required", nil)
			return
		}
		var req invalidateCacheRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		n, err := d.Cache.Invalidate(r.Context(), cacheifaces.Prefix{
			TenantID: id.TenantID,
			Alias:    req.Alias,
			ScopeID:  req.ScopeID,
			All:      req.All,
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "invalidate_failed", err.Error(), nil)
			return
		}
		emitWithActor(d, r, "llm.cache_invalidated", id.TenantID, map[string]any{
			"alias": req.Alias, "scope_id": req.ScopeID, "all": req.All, "removed": n,
		})
		writeJSON(w, http.StatusOK, map[string]any{"removed": n})
	}
}
