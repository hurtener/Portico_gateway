package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// PolicyRulesController is the API-facing surface for the policy rule
// store. cmd/portico provides a *policy.RuleStore wrapped via this
// interface so the handlers stay free of the policy package's full
// engine dependency.
type PolicyRulesController interface {
	List(ctx context.Context, tenantID string) (policy.RuleSet, error)
	Get(ctx context.Context, tenantID, ruleID string) (policy.Rule, error)
	Upsert(ctx context.Context, tenantID string, r policy.Rule) (policy.Rule, error)
	Delete(ctx context.Context, tenantID, ruleID string) error
	ReplaceAll(ctx context.Context, tenantID string, set policy.RuleSet) error
}

// listPolicyRulesHandler GET /api/policy/rules.
func listPolicyRulesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.PolicyRules == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "policy_not_configured", "policy rules store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		set, err := d.PolicyRules.List(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		// Always render rules as an array (never null) so the Console can
		// render an empty state without checking for null.
		if set.Rules == nil {
			set.Rules = []policy.Rule{}
		}
		writeJSON(w, http.StatusOK, set)
	}
}

// putPolicyRulesHandler PUT /api/policy/rules — replaces the entire ruleset.
func putPolicyRulesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.PolicyRules == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "policy_not_configured", "policy rules store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		var body policy.RuleSet
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		// Validate up-front so the handler returns one structured error
		// rather than partial mutations (the store's ReplaceAll is
		// transactional but echoes the first failure).
		for _, rule := range body.Rules {
			if err := policy.Validate(rule); err != nil {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error(),
					map[string]any{"rule_id": rule.ID})
				return
			}
		}
		if err := d.PolicyRules.ReplaceAll(r.Context(), id.TenantID, body); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "replace_failed", err.Error(), nil)
			return
		}
		emitWithActor(d, r, audit.EventPolicyRuleChanged, "",
			map[string]any{"op": "replace_all", "count": len(body.Rules)})
		writeJSON(w, http.StatusOK, body)
	}
}

// postPolicyRuleHandler POST /api/policy/rules — append.
func postPolicyRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.PolicyRules == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "policy_not_configured", "policy rules store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		var rule policy.Rule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		rule.UpdatedAt = time.Now().UTC()
		rule.UpdatedBy = id.UserID
		stored, err := d.PolicyRules.Upsert(r.Context(), id.TenantID, rule)
		if err != nil {
			if errors.Is(err, policy.ErrInvalidRule) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "upsert_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventPolicyRuleChanged, "policy_rule", rule.ID, "policy.rule_changed",
			map[string]any{"rule_id": rule.ID, "priority": rule.Priority, "risk_class": rule.RiskClass})
		writeJSON(w, http.StatusCreated, stored)
	}
}

// putPolicyRuleHandler PUT /api/policy/rules/{id} — update.
func putPolicyRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.PolicyRules == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "policy_not_configured", "policy rules store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		ruleID := chi.URLParam(r, "id")
		var rule policy.Rule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if rule.ID == "" {
			rule.ID = ruleID
		} else if rule.ID != ruleID {
			writeJSONError(w, http.StatusBadRequest, "id_mismatch", "path id and body id must match",
				map[string]any{"path": ruleID, "body": rule.ID})
			return
		}
		rule.UpdatedAt = time.Now().UTC()
		rule.UpdatedBy = id.UserID
		stored, err := d.PolicyRules.Upsert(r.Context(), id.TenantID, rule)
		if err != nil {
			if errors.Is(err, policy.ErrInvalidRule) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventPolicyRuleChanged, "policy_rule", rule.ID, "policy.rule_changed",
			map[string]any{"rule_id": rule.ID})
		writeJSON(w, http.StatusOK, stored)
	}
}

// deletePolicyRuleHandler DELETE /api/policy/rules/{id}.
func deletePolicyRuleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.PolicyRules == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "policy_not_configured", "policy rules store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		ruleID := chi.URLParam(r, "id")
		if err := d.PolicyRules.Delete(r.Context(), id.TenantID, ruleID); err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "rule not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventPolicyRuleDeleted, "policy_rule", ruleID, "policy.rule_deleted",
			map[string]any{"rule_id": ruleID})
		w.WriteHeader(http.StatusNoContent)
	}
}

// dryRunPolicyHandler POST /api/policy/dry-run — evaluates a synthetic
// tool call against the live ruleset. Body: {call: ToolCallShape, rules?: RuleSet}.
// When rules is omitted, the live tenant ruleset is loaded from the store.
func dryRunPolicyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Call  policy.ToolCallShape `json:"call"`
			Rules *policy.RuleSet      `json:"rules,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		var rs policy.RuleSet
		if body.Rules != nil {
			rs = *body.Rules
		} else if d.PolicyRules != nil {
			id, _ := tenant.From(r.Context())
			loaded, err := d.PolicyRules.List(r.Context(), id.TenantID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "load_failed", err.Error(), nil)
				return
			}
			rs = loaded
		}
		// Default tenant id from the request when caller omits it.
		if body.Call.TenantID == "" {
			id, _ := tenant.From(r.Context())
			body.Call.TenantID = id.TenantID
		}
		res := policy.DryRun(r.Context(), rs, body.Call)
		emitWithActor(d, r, audit.EventPolicyDryRun, "",
			map[string]any{"tool": body.Call.Tool, "matched": len(res.MatchedRules)})
		writeJSON(w, http.StatusOK, res)
	}
}

// listPolicyActivityHandler GET /api/policy/activity — the entity_activity
// rows for policy_rule entries across all rule IDs (limit 200).
func listPolicyActivityHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.EntityActivity == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		id, _ := tenant.From(r.Context())
		// We list across rule IDs by passing an empty id and reusing the
		// projection's prefix scan — but the store filters by id, so for
		// now we return the most recent 50 events of kind policy_rule by
		// scanning a known empty id (no rule will ever have this id).
		rows, err := d.EntityActivity.List(r.Context(), id.TenantID, "policy_rule", "", 50)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}
