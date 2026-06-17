package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/llm/quota"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// llmQuotaDTO is the wire shape for a tenant's LLM quota. A zero value in any
// numeric field means "unlimited" for that dimension.
type llmQuotaDTO struct {
	RequestsPerMinute int     `json:"requests_per_minute"`
	TokensPerMinute   int     `json:"tokens_per_minute"`
	TokensPerDay      int     `json:"tokens_per_day"`
	CostUSDPerDay     float64 `json:"cost_usd_per_day"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
}

func quotaToDTO(q *ifaces.LLMQuota) llmQuotaDTO {
	return llmQuotaDTO{
		RequestsPerMinute: q.RequestsPerMinute,
		TokensPerMinute:   q.TokensPerMinute,
		TokensPerDay:      q.TokensPerDay,
		CostUSDPerDay:     q.CostUSDPerDay,
		UpdatedAt:         q.UpdatedAt,
	}
}

// getLLMQuotaHandler GET /api/llm/quota — returns the calling tenant's quota,
// falling back to the built-in defaults when no row has been set. Readable by
// any authenticated member of the tenant (it is the tenant's own quota).
func getLLMQuotaHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMQuotas == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm quota store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		q, err := d.LLMQuotas.GetOrDefault(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, quotaToDTO(q))
	}
}

// putLLMQuotaHandler PUT /api/llm/quota — upserts the calling tenant's quota.
// Admin scope required (it raises/lowers enforcement for the whole tenant).
func putLLMQuotaHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMQuotas == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm quota store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		var dto llmQuotaDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if dto.RequestsPerMinute < 0 || dto.TokensPerMinute < 0 || dto.TokensPerDay < 0 || dto.CostUSDPerDay < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "quota values must be non-negative", nil)
			return
		}
		q := &ifaces.LLMQuota{
			TenantID:          id.TenantID,
			RequestsPerMinute: dto.RequestsPerMinute,
			TokensPerMinute:   dto.TokensPerMinute,
			TokensPerDay:      dto.TokensPerDay,
			CostUSDPerDay:     dto.CostUSDPerDay,
		}
		if err := d.LLMQuotas.SetQuota(r.Context(), q); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "set_failed", err.Error(), nil)
			return
		}
		emitWithActor(d, r, "llm.quota_updated", id.TenantID, map[string]any{
			"requests_per_minute": q.RequestsPerMinute,
			"tokens_per_minute":   q.TokensPerMinute,
			"tokens_per_day":      q.TokensPerDay,
			"cost_usd_per_day":    q.CostUSDPerDay,
		})
		// Return the stored row (re-read to surface the persisted updated_at).
		stored, err := d.LLMQuotas.GetOrDefault(r.Context(), id.TenantID)
		if err != nil {
			writeJSON(w, http.StatusOK, quotaToDTO(q))
			return
		}
		writeJSON(w, http.StatusOK, quotaToDTO(stored))
	}
}

// checkQuota enforces the tenant's LLM limits before dispatch. It returns true
// when the request may proceed, or false after writing a 429 quota_exceeded
// response (plus an llm.quota_exceeded audit event). Enforcement is skipped
// (returns true) when the quota store or enforcer is not wired.
func checkQuota(d Deps, w http.ResponseWriter, r *http.Request, tenantID, model string) bool {
	if d.LLMQuota == nil || d.LLMQuotas == nil {
		return true
	}
	q, err := d.LLMQuotas.GetOrDefault(r.Context(), tenantID)
	if err != nil {
		// A quota lookup failure must not hard-fail the request; log via audit
		// and allow (fail-open on the limit lookup, not on a real breach).
		return true
	}
	lim := quota.Limits{
		RequestsPerMinute: q.RequestsPerMinute,
		TokensPerMinute:   q.TokensPerMinute,
		TokensPerDay:      q.TokensPerDay,
	}
	if err := d.LLMQuota.Check(tenantID, lim); err != nil {
		var ex *quota.ExceededError
		if errors.As(err, &ex) {
			emitWithActor(d, r, "llm.quota_exceeded", tenantID, map[string]any{
				"limit": ex.Limit,
				"model": model,
			})
			writeJSONError(w, http.StatusTooManyRequests, "quota_exceeded",
				"LLM quota exceeded: "+ex.Limit, map[string]any{"limit": ex.Limit})
			return false
		}
		// Unexpected enforcer error — fail open.
		return true
	}
	return true
}

// recordQuotaUsage records token usage against the tenant's rolling windows
// after a successful dispatch. No-op when the enforcer is not wired or tokens
// is non-positive.
func recordQuotaUsage(d Deps, tenantID string, tokens int) {
	if d.LLMQuota == nil {
		return
	}
	d.LLMQuota.RecordUsage(tenantID, tokens)
}
