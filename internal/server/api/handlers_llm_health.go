package api

import (
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// llmProviderHealthDTO is one row of the LLM health view: a tenant-configured
// provider cross-referenced with the engine's live view of its driver.
type llmProviderHealthDTO struct {
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Enabled bool   `json:"enabled"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

// getLLMHealthHandler GET /api/llm/health — returns, for each provider the
// tenant has configured, whether the engine reports its driver healthy. A
// disabled provider is reported unhealthy with a "disabled" detail (it is not
// routable), independent of the engine's view.
func getLLMHealthHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMEngine == nil || d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm gateway not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		provs, err := d.LLMProviders.ListProviders(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		// Engine health is keyed by driver. A lookup miss means the engine does
		// not know that driver — reported unhealthy with a clear detail.
		byDriver := map[string]string{}
		healthyDriver := map[string]bool{}
		if eh, herr := d.LLMEngine.Health(r.Context()); herr == nil {
			for _, h := range eh {
				healthyDriver[h.Driver] = h.Healthy
				byDriver[h.Driver] = h.Detail
			}
		}

		out := make([]llmProviderHealthDTO, 0, len(provs))
		for _, p := range provs {
			row := llmProviderHealthDTO{Name: p.Name, Driver: p.Driver, Enabled: p.Enabled}
			switch {
			case !p.Enabled:
				row.Healthy = false
				row.Detail = "disabled"
			case healthyDriver[p.Driver]:
				row.Healthy = true
				row.Detail = orDefault(byDriver[p.Driver], "configured")
			default:
				row.Healthy = false
				if det, ok := byDriver[p.Driver]; ok {
					row.Detail = orDefault(det, "driver unhealthy")
				} else {
					row.Detail = "driver not loaded in engine"
				}
			}
			out = append(out, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": out})
	}
}
