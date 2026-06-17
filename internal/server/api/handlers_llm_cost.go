package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// recordCost accumulates a single dispatch into the tenant's daily cost rollup.
// It looks up the unit price for (driver, providerModel) from the global price
// book; an unpriced model still records request + token counts with cost 0 so
// usage is visible even before prices are configured. No-op when the cost store
// is not wired. Cost recording must never fail the request — errors are
// swallowed (best-effort telemetry).
func recordCost(d Deps, r *http.Request, tenantID, alias, driver, providerModel string, inputTok, outputTok int) {
	if d.LLMCosts == nil {
		return
	}
	ctx := r.Context()
	var costUSD float64
	if uc, err := d.LLMCosts.GetUnitCost(ctx, driver, providerModel); err == nil && uc != nil {
		costUSD = float64(inputTok)/1000.0*uc.InputPer1K + float64(outputTok)/1000.0*uc.OutputPer1K
	}
	day := time.Now().UTC().Format("2006-01-02")
	_ = d.LLMCosts.AddUsage(ctx, tenantID, day, alias, 1, inputTok, outputTok, costUSD)
}

// --- cost query DTOs ---

type llmCostDailyDTO struct {
	Day       string  `json:"day"`
	Alias     string  `json:"alias"`
	Requests  int     `json:"requests"`
	InputTok  int     `json:"input_tokens"`
	OutputTok int     `json:"output_tokens"`
	CostUSD   float64 `json:"cost_usd"`
}

type llmCostSummaryDTO struct {
	Requests  int     `json:"requests"`
	InputTok  int     `json:"input_tokens"`
	OutputTok int     `json:"output_tokens"`
	CostUSD   float64 `json:"cost_usd"`
}

type llmCostsResponseDTO struct {
	From    string            `json:"from,omitempty"`
	To      string            `json:"to,omitempty"`
	Summary llmCostSummaryDTO `json:"summary"`
	Daily   []llmCostDailyDTO `json:"daily"`
}

// listLLMCostsHandler GET /api/llm/costs?from=YYYY-MM-DD&to=YYYY-MM-DD — returns
// the calling tenant's daily cost rollups plus a roll-up summary over the range.
func listLLMCostsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMCosts == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm cost store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		if !validDayParam(from) || !validDayParam(to) {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "from/to must be YYYY-MM-DD", nil)
			return
		}
		rows, err := d.LLMCosts.ListDaily(r.Context(), id.TenantID, from, to)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		resp := llmCostsResponseDTO{From: from, To: to, Daily: make([]llmCostDailyDTO, 0, len(rows))}
		for _, c := range rows {
			resp.Daily = append(resp.Daily, llmCostDailyDTO{
				Day: c.Day, Alias: c.Alias, Requests: c.Requests,
				InputTok: c.InputTok, OutputTok: c.OutputTok, CostUSD: c.CostUSD,
			})
			resp.Summary.Requests += c.Requests
			resp.Summary.InputTok += c.InputTok
			resp.Summary.OutputTok += c.OutputTok
			resp.Summary.CostUSD += c.CostUSD
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// validDayParam accepts an empty string (unbounded) or a strict YYYY-MM-DD date.
func validDayParam(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// --- price book DTOs ---

type llmUnitCostDTO struct {
	ProviderDriver string  `json:"provider_driver"`
	ProviderModel  string  `json:"provider_model"`
	InputPer1K     float64 `json:"input_per_1k"`
	OutputPer1K    float64 `json:"output_per_1k"`
}

// listLLMPricesHandler GET /api/llm/costs/prices — returns the global price book.
func listLLMPricesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMCosts == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm cost store not configured", nil)
			return
		}
		prices, err := d.LLMCosts.ListUnitCosts(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]llmUnitCostDTO, 0, len(prices))
		for _, p := range prices {
			out = append(out, llmUnitCostDTO{
				ProviderDriver: p.ProviderDriver, ProviderModel: p.ProviderModel,
				InputPer1K: p.InputPer1K, OutputPer1K: p.OutputPer1K,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"prices": out})
	}
}

// putLLMPriceHandler PUT /api/llm/costs/prices — upserts a unit price. The price
// book is global (not tenant-scoped), so it requires admin scope.
func putLLMPriceHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMCosts == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm cost store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		var dto llmUnitCostDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if dto.ProviderDriver == "" || dto.ProviderModel == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "provider_driver and provider_model are required", nil)
			return
		}
		if dto.InputPer1K < 0 || dto.OutputPer1K < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "prices must be non-negative", nil)
			return
		}
		uc := &ifaces.LLMUnitCost{
			ProviderDriver: dto.ProviderDriver, ProviderModel: dto.ProviderModel,
			InputPer1K: dto.InputPer1K, OutputPer1K: dto.OutputPer1K,
		}
		if err := d.LLMCosts.SetUnitCost(r.Context(), uc); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "set_failed", err.Error(), nil)
			return
		}
		emitWithActor(d, r, "llm.price_updated", id.TenantID, map[string]any{
			"provider_driver": uc.ProviderDriver,
			"provider_model":  uc.ProviderModel,
			"input_per_1k":    uc.InputPer1K,
			"output_per_1k":   uc.OutputPer1K,
		})
		writeJSON(w, http.StatusOK, dto)
	}
}
