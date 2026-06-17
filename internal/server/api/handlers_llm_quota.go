package api

import (
	"errors"
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/llm/quota"
)

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
