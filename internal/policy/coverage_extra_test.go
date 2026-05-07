package policy

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// Phase 9 follow-up: lift internal/policy past the 80% gate by
// covering dryrun edge cases and editor encode/decode round-trips.

// ---- editor.go ----

func TestEncodeDecodeConditions_RoundTrip(t *testing.T) {
	c := Conditions{Match: Match{
		Tools:     []string{"github.list_repos", "github.delete_repo"},
		Servers:   []string{"github"},
		Tenants:   []string{"acme"},
		ArgsExpr:  "env=prod",
		TimeRange: TimeRange{From: "09:00", To: "17:00"},
	}}
	b, err := EncodeConditions(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeConditions(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Match.Tools, c.Match.Tools) {
		t.Errorf("tools mismatch: %+v", got)
	}
	if got.Match.ArgsExpr != "env=prod" || got.Match.TimeRange.From != "09:00" {
		t.Errorf("conditions decode lost fields: %+v", got)
	}
}

func TestEncodeDecodeActions_RoundTrip(t *testing.T) {
	a := Actions{
		Allow: true, RequireApproval: true,
		LogLevel: "audit", AnnotateRiskClass: RiskWrite,
	}
	b, err := EncodeActions(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeActions(b)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Allow || !got.RequireApproval || got.LogLevel != "audit" || got.AnnotateRiskClass != RiskWrite {
		t.Errorf("actions decode lost fields: %+v", got)
	}
}

func TestDecodeConditions_EmptyBytes(t *testing.T) {
	c, err := DecodeConditions(nil)
	if err != nil {
		t.Errorf("empty bytes should yield empty Conditions, got err=%v", err)
	}
	if c.Match.ArgsExpr != "" {
		t.Errorf("expected zero Conditions, got %+v", c)
	}
}

func TestDecodeActions_EmptyBytes(t *testing.T) {
	a, err := DecodeActions(nil)
	if err != nil {
		t.Errorf("empty bytes should yield empty Actions, got err=%v", err)
	}
	if a.Allow || a.Deny {
		t.Errorf("expected zero Actions, got %+v", a)
	}
}

func TestDecodeConditions_BadJSON(t *testing.T) {
	if _, err := DecodeConditions([]byte("not-json")); err == nil {
		t.Errorf("expected decode error")
	}
}

func TestDecodeActions_BadJSON(t *testing.T) {
	if _, err := DecodeActions([]byte("not-json")); err == nil {
		t.Errorf("expected decode error")
	}
}

func TestValidate_TimeRangeToInvalid(t *testing.T) {
	r := Rule{ID: "r", Conditions: Conditions{Match: Match{TimeRange: TimeRange{To: "noon"}}}}
	if err := Validate(r); err == nil {
		t.Errorf("expected error for invalid To")
	}
}

func TestCanonicalise_EmptyRuleSet(t *testing.T) {
	b, err := Canonicalise(RuleSet{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Errorf("canonicalise of empty produced no output")
	}
}

// ---- dryrun.go ----

func TestDryRun_DisabledRulesIgnored(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "off", Priority: 1, Enabled: false,
			Conditions: Conditions{Match: Match{Tools: []string{"x.y"}}},
			Actions:    Actions{Deny: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y"})
	if len(res.MatchedRules) != 0 {
		t.Errorf("disabled rule should not match: %+v", res)
	}
}

func TestDryRun_TimeRangeMissReturnsNoMatch(t *testing.T) {
	out, _ := time.Parse(time.RFC3339, "2026-01-01T22:00:00Z")
	rs := RuleSet{Rules: []Rule{
		{ID: "biz-hours", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{TimeRange: TimeRange{From: "09:00", To: "17:00"}}},
			Actions:    Actions{Allow: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y", Now: out})
	if len(res.MatchedRules) != 0 {
		t.Errorf("expected miss, got %+v", res.MatchedRules)
	}
}

func TestDryRun_TimeRangeWrapsMidnight(t *testing.T) {
	// Window 22:00..06:00 — 03:00 UTC is inside the wrap-window.
	at, _ := time.Parse(time.RFC3339, "2026-01-01T03:00:00Z")
	rs := RuleSet{Rules: []Rule{
		{ID: "night", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{TimeRange: TimeRange{From: "22:00", To: "06:00"}}},
			Actions:    Actions{Allow: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y", Now: at})
	if len(res.MatchedRules) != 1 {
		t.Errorf("expected wrap-around match, got %+v", res)
	}
}

func TestDryRun_AnnotateOnly_Sticky_DoesNotShortCircuit(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "annotate", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"x.y"}}},
			Actions:    Actions{AnnotateRiskClass: RiskDestructive, LogLevel: "audit"}},
		{ID: "allow-all", Priority: 50, Enabled: true,
			Conditions: Conditions{Match: Match{}},
			Actions:    Actions{Allow: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y"})
	// First rule wins on the verdict (no allow/deny but annotate sticks).
	if res.FinalAction.AnnotateRiskClass != RiskDestructive {
		t.Errorf("annotate didn't stick: %+v", res.FinalAction)
	}
	if res.FinalAction.LogLevel != "audit" {
		t.Errorf("log level didn't stick: %+v", res.FinalAction)
	}
	// The second rule that matches is recorded as a sticky annotation
	// when the first rule already won.
	if len(res.MatchedRules) == 0 {
		t.Errorf("expected matched rules: %+v", res)
	}
}

func TestDryRun_TenantsMatch(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "only-acme", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tenants: []string{"acme"}}},
			Actions:    Actions{Allow: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{TenantID: "acme", Server: "x", Tool: "x.y"})
	if !hit.FinalAction.Allow {
		t.Errorf("expected match for acme: %+v", hit)
	}
	miss := DryRun(context.Background(), rs, ToolCallShape{TenantID: "beta", Server: "x", Tool: "x.y"})
	if miss.FinalAction.Allow {
		t.Errorf("expected miss for beta: %+v", miss)
	}
}

func TestDryRun_ServersMatch(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "github-only", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Servers: []string{"github"}}},
			Actions:    Actions{Allow: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{Server: "github", Tool: "x.y"})
	if !hit.FinalAction.Allow {
		t.Errorf("expected match: %+v", hit)
	}
	miss := DryRun(context.Background(), rs, ToolCallShape{Server: "gitlab", Tool: "x.y"})
	if miss.FinalAction.Allow {
		t.Errorf("expected miss: %+v", miss)
	}
}

func TestDryRun_ToolGlob(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "any-list", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"github.list_*"}}},
			Actions:    Actions{Allow: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{Server: "github", Tool: "github.list_repos"})
	if !hit.FinalAction.Allow {
		t.Errorf("expected glob match: %+v", hit)
	}
}

func TestDryRun_ToolWildcard(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "match-all", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"*"}}},
			Actions:    Actions{Allow: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y"})
	if !hit.FinalAction.Allow {
		t.Errorf("expected wildcard match: %+v", hit)
	}
}

func TestDryRun_ArgsExprBadShape(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "bad-expr", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"x.y"}, ArgsExpr: "noequals"}},
			Actions:    Actions{Deny: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y", Args: map[string]any{"k": "v"}})
	if res.FinalAction.Deny {
		t.Errorf("expected no match for malformed args expr: %+v", res)
	}
}

func TestDryRun_ArgsExprMultiplePairs(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "two-pairs", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{
				Tools:    []string{"x.y"},
				ArgsExpr: "env=prod, region=us",
			}},
			Actions: Actions{Deny: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y",
		Args: map[string]any{"env": "prod", "region": "us"}})
	if !hit.FinalAction.Deny {
		t.Errorf("expected hit when both args match: %+v", hit)
	}
	miss := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y",
		Args: map[string]any{"env": "prod", "region": "eu"}})
	if miss.FinalAction.Deny {
		t.Errorf("expected miss when one arg differs: %+v", miss)
	}
}

func TestDryRun_ArgsExpr_BoolValue(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "bool", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{
				Tools:    []string{"x.y"},
				ArgsExpr: "force=true",
			}},
			Actions: Actions{Deny: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y",
		Args: map[string]any{"force": true}})
	if !hit.FinalAction.Deny {
		t.Errorf("expected hit for bool=true match")
	}
	miss := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y",
		Args: map[string]any{"force": false}})
	if miss.FinalAction.Deny {
		t.Errorf("expected miss for bool=false")
	}
}

func TestDryRun_ArgsExpr_IntValue(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "int", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{
				Tools:    []string{"x.y"},
				ArgsExpr: "count=5",
			}},
			Actions: Actions{Deny: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y",
		Args: map[string]any{"count": 5}})
	if !hit.FinalAction.Deny {
		t.Errorf("expected hit for int match: %+v", hit)
	}
}

func TestDryRun_FinalRiskDefaultsWrite(t *testing.T) {
	res := DryRun(context.Background(), RuleSet{}, ToolCallShape{Server: "x", Tool: "x.y"})
	if res.FinalRisk != RiskWrite {
		t.Errorf("expected default risk write, got %q", res.FinalRisk)
	}
}

// Direct contains/inTimeRange unit tests to bring those helpers above 0%.
func TestDryRun_Helpers(t *testing.T) {
	if !contains([]string{"a", "b"}, "a") {
		t.Errorf("contains miss")
	}
	if contains([]string{"a"}, "x") {
		t.Errorf("contains false positive")
	}
	now, _ := time.Parse(time.RFC3339, "2026-01-01T10:00:00Z")
	if !inTimeRange(now, "09:00", "17:00") {
		t.Errorf("inTimeRange should match in window")
	}
	if !inTimeRange(now, "", "") {
		t.Errorf("empty bounds should always match")
	}
}
