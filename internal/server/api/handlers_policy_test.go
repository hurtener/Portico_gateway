package api

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 9 policy handlers: list, replace-all (PUT), append (POST),
// update-by-id (PUT /{id}), delete-by-id, dry-run, activity.

func goodRule(id string) policy.Rule {
	return policy.Rule{
		ID: id, Priority: 10, Enabled: true, RiskClass: policy.RiskRead,
		Actions: policy.Actions{Allow: true},
	}
}

func TestListPolicyRules_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, rules, _, _ := testDeps(t)
	_, _ = rules.Upsert(context.Background(), "t1", goodRule("a"))
	r := newReq("GET", "/api/policy/rules", nil)
	w := runHandler(listPolicyRulesHandler(d), r)
	statusOK(t, w, 200)
	var got policy.RuleSet
	decodeJSON(t, w, &got)
	if len(got.Rules) != 1 {
		t.Errorf("want 1 rule, got %d", len(got.Rules))
	}
}

func TestListPolicyRules_NeverNullArray(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/api/policy/rules", nil)
	w := runHandler(listPolicyRulesHandler(d), r)
	statusOK(t, w, 200)
	body := w.Body.String()
	if body == "null\n" || body == "" {
		t.Errorf("expected non-null body, got %q", body)
	}
}

func TestListPolicyRules_NotConfigured(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.PolicyRules = nil
	r := newReq("GET", "/api/policy/rules", nil)
	w := runHandler(listPolicyRulesHandler(d), r)
	statusOK(t, w, 503)
}

func TestPutPolicyRules_ReplacesAndEmits(t *testing.T) {
	d, em, _, _, _, _, _, rules, _, _ := testDeps(t)
	body := policy.RuleSet{Rules: []policy.Rule{goodRule("x"), goodRule("y")}}
	r := newReq("PUT", "/api/policy/rules", body)
	w := runHandler(putPolicyRulesHandler(d), r)
	statusOK(t, w, 200)
	got, _ := rules.List(context.Background(), "t1")
	if len(got.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(got.Rules))
	}
	if !hasEvent(em, audit.EventPolicyRuleChanged) {
		t.Errorf("missing policy.rule_changed event")
	}
}

func TestPutPolicyRules_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/api/policy/rules", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(putPolicyRulesHandler(d), r)
	statusOK(t, w, 400)
}

func TestPutPolicyRules_ValidationFailure(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	// Empty ID fails validation.
	body := policy.RuleSet{Rules: []policy.Rule{{ID: "", Actions: policy.Actions{Allow: true}}}}
	r := newReq("PUT", "/api/policy/rules", body)
	w := runHandler(putPolicyRulesHandler(d), r)
	statusOK(t, w, 400)
	if got := readErrorCode(t, w); got != "validation_failed" {
		t.Errorf("want validation_failed, got %q", got)
	}
}

func TestPutPolicyRules_NotConfigured(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.PolicyRules = nil
	r := newReq("PUT", "/api/policy/rules", policy.RuleSet{})
	w := runHandler(putPolicyRulesHandler(d), r)
	statusOK(t, w, 503)
}

func TestPostPolicyRule_HappyPath(t *testing.T) {
	d, em, _, _, _, _, _, _, _, _ := testDeps(t)
	rule := goodRule("z")
	r := newReq("POST", "/api/policy/rules", rule)
	w := runHandler(postPolicyRuleHandler(d), r)
	statusOK(t, w, 201)
	if !hasEvent(em, audit.EventPolicyRuleChanged) {
		t.Errorf("missing policy.rule_changed event")
	}
}

func TestPostPolicyRule_ValidationFails(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	rule := policy.Rule{ID: "", Actions: policy.Actions{Allow: true, Deny: true}} // bad
	r := newReq("POST", "/api/policy/rules", rule)
	w := runHandler(postPolicyRuleHandler(d), r)
	statusOK(t, w, 400)
}

func TestPostPolicyRule_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/policy/rules", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(postPolicyRuleHandler(d), r)
	statusOK(t, w, 400)
}

func TestPostPolicyRule_NotConfigured(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.PolicyRules = nil
	r := newReq("POST", "/api/policy/rules", goodRule("z"))
	w := runHandler(postPolicyRuleHandler(d), r)
	statusOK(t, w, 503)
}

func TestPutPolicyRule_HappyPath(t *testing.T) {
	d, em, _, _, _, _, _, rules, _, _ := testDeps(t)
	_, _ = rules.Upsert(context.Background(), "t1", goodRule("z"))
	rule := goodRule("z")
	rule.Notes = "updated note"
	r := newReq("PUT", "/api/policy/rules/z", rule)
	r = withChiURLParam(r, "id", "z")
	w := runHandler(putPolicyRuleHandler(d), r)
	statusOK(t, w, 200)
	if !hasEvent(em, audit.EventPolicyRuleChanged) {
		t.Errorf("missing policy.rule_changed event")
	}
}

func TestPutPolicyRule_IDMismatch(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	rule := goodRule("body-id")
	r := newReq("PUT", "/api/policy/rules/url-id", rule)
	r = withChiURLParam(r, "id", "url-id")
	w := runHandler(putPolicyRuleHandler(d), r)
	statusOK(t, w, 400)
	if got := readErrorCode(t, w); got != "id_mismatch" {
		t.Errorf("want id_mismatch, got %q", got)
	}
}

func TestPutPolicyRule_FillsIDFromPath(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	rule := policy.Rule{Priority: 1, Enabled: true, RiskClass: policy.RiskRead, Actions: policy.Actions{Allow: true}}
	r := newReq("PUT", "/api/policy/rules/auto", rule)
	r = withChiURLParam(r, "id", "auto")
	w := runHandler(putPolicyRuleHandler(d), r)
	statusOK(t, w, 200)
}

func TestPutPolicyRule_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/api/policy/rules/z", nil)
	r.Body = httpReadCloser("not-json")
	r = withChiURLParam(r, "id", "z")
	w := runHandler(putPolicyRuleHandler(d), r)
	statusOK(t, w, 400)
}

func TestPutPolicyRule_ValidationFails(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	rule := policy.Rule{ID: "z", Actions: policy.Actions{Allow: true, Deny: true}}
	r := newReq("PUT", "/api/policy/rules/z", rule)
	r = withChiURLParam(r, "id", "z")
	w := runHandler(putPolicyRuleHandler(d), r)
	statusOK(t, w, 400)
}

func TestPutPolicyRule_NotConfigured(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.PolicyRules = nil
	r := newReq("PUT", "/api/policy/rules/z", goodRule("z"))
	r = withChiURLParam(r, "id", "z")
	w := runHandler(putPolicyRuleHandler(d), r)
	statusOK(t, w, 503)
}

func TestDeletePolicyRule_HappyPath(t *testing.T) {
	d, em, _, _, _, _, _, rules, _, _ := testDeps(t)
	_, _ = rules.Upsert(context.Background(), "t1", goodRule("z"))
	r := newReq("DELETE", "/api/policy/rules/z", nil)
	r = withChiURLParam(r, "id", "z")
	w := runHandler(deletePolicyRuleHandler(d), r)
	statusOK(t, w, 204)
	if !hasEvent(em, audit.EventPolicyRuleDeleted) {
		t.Errorf("missing policy.rule_deleted event")
	}
}

func TestDeletePolicyRule_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("DELETE", "/api/policy/rules/missing", nil)
	r = withChiURLParam(r, "id", "missing")
	w := runHandler(deletePolicyRuleHandler(d), r)
	statusOK(t, w, 404)
}

func TestDeletePolicyRule_NotConfigured(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.PolicyRules = nil
	r := newReq("DELETE", "/api/policy/rules/z", nil)
	r = withChiURLParam(r, "id", "z")
	w := runHandler(deletePolicyRuleHandler(d), r)
	statusOK(t, w, 503)
}

func TestDryRunPolicy_LiveRuleset(t *testing.T) {
	d, em, _, _, _, _, _, rules, _, _ := testDeps(t)
	_, _ = rules.Upsert(context.Background(), "t1", policy.Rule{
		ID: "deny", Priority: 1, Enabled: true,
		Conditions: policy.Conditions{Match: policy.Match{Tools: []string{"x.y"}}},
		Actions:    policy.Actions{Deny: true},
	})
	body := map[string]any{
		"call": map[string]any{"server": "x", "tool": "x.y"},
	}
	r := newReq("POST", "/api/policy/dry-run", body)
	w := runHandler(dryRunPolicyHandler(d), r)
	statusOK(t, w, 200)
	var got policy.DryRunResult
	decodeJSON(t, w, &got)
	if !got.FinalAction.Deny {
		t.Errorf("expected deny verdict, got %+v", got.FinalAction)
	}
	if !hasEvent(em, audit.EventPolicyDryRun) {
		t.Errorf("missing policy.dry_run event")
	}
}

func TestDryRunPolicy_InlineRuleset(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	body := map[string]any{
		"call": map[string]any{"server": "x", "tool": "x.y"},
		"rules": map[string]any{
			"rules": []map[string]any{{
				"id": "allow", "priority": 1, "enabled": true,
				"conditions": map[string]any{"match": map[string]any{"tools": []string{"x.y"}}},
				"actions":    map[string]any{"allow": true},
			}},
		},
	}
	r := newReq("POST", "/api/policy/dry-run", body)
	w := runHandler(dryRunPolicyHandler(d), r)
	statusOK(t, w, 200)
}

func TestDryRunPolicy_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/policy/dry-run", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(dryRunPolicyHandler(d), r)
	statusOK(t, w, 400)
}

func TestPolicyActivity_NoStore_EmptyArray(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.EntityActivity = nil
	r := newReq("GET", "/api/policy/activity", nil)
	w := runHandler(listPolicyActivityHandler(d), r)
	statusOK(t, w, 200)
}

func TestPolicyActivity_HappyPath(t *testing.T) {
	d, _, _, activity, _, _, _, _, _, _ := testDeps(t)
	if err := activity.Append(context.Background(), &ifaces.EntityActivityRecord{
		TenantID:    "t1",
		EntityKind:  "policy_rule",
		EntityID:    "",
		EventID:     "ev1",
		OccurredAt:  time.Now().UTC(),
		ActorUserID: "u1",
		Summary:     "policy.rule_changed",
	}); err != nil {
		t.Fatal(err)
	}
	r := newReq("GET", "/api/policy/activity", nil)
	w := runHandler(listPolicyActivityHandler(d), r)
	statusOK(t, w, 200)
}
