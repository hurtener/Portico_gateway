package policy

import (
	"context"
	"testing"
	"time"
)

func TestDryRun_HappyPath_Allow(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{
			ID: "allow-read", Priority: 10, Enabled: true, RiskClass: RiskRead,
			Conditions: Conditions{Match: Match{Tools: []string{"github.list_repos"}}},
			Actions:    Actions{Allow: true},
		},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{
		TenantID: "acme", Server: "github", Tool: "github.list_repos",
	})
	if !res.FinalAction.Allow || res.FinalAction.Deny {
		t.Errorf("expected allow, got %+v", res.FinalAction)
	}
	if len(res.MatchedRules) != 1 {
		t.Errorf("expected 1 matched rule, got %d", len(res.MatchedRules))
	}
}

func TestDryRun_DenyOverridesAllow(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "deny-first", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"github.delete_repo"}}},
			Actions:    Actions{Deny: true}},
		{ID: "allow-all", Priority: 100, Enabled: true,
			Conditions: Conditions{Match: Match{}},
			Actions:    Actions{Allow: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{
		TenantID: "acme", Server: "github", Tool: "github.delete_repo",
	})
	if !res.FinalAction.Deny || res.FinalAction.Allow {
		t.Errorf("expected deny verdict, got %+v", res.FinalAction)
	}
}

func TestDryRun_PriorityWins(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "allow-late", Priority: 50, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"x.y"}}},
			Actions:    Actions{Allow: true}},
		{ID: "deny-early", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"x.y"}}},
			Actions:    Actions{Deny: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{
		TenantID: "acme", Server: "x", Tool: "x.y",
	})
	if !res.FinalAction.Deny {
		t.Errorf("expected deny by priority, got %+v", res.FinalAction)
	}
	if len(res.LosingRules) != 1 || res.LosingRules[0].RuleID != "allow-late" {
		t.Errorf("expected allow-late as losing rule, got %+v", res.LosingRules)
	}
}

func TestDryRun_TimeRangeMatch(t *testing.T) {
	now, _ := time.Parse(time.RFC3339, "2026-01-01T10:30:00Z")
	rs := RuleSet{Rules: []Rule{
		{ID: "biz-hours", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{TimeRange: TimeRange{From: "09:00", To: "17:00"}}},
			Actions:    Actions{Allow: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{
		TenantID: "acme", Server: "x", Tool: "x.y", Now: now,
	})
	if !res.FinalAction.Allow {
		t.Errorf("expected match in business hours, got %+v", res)
	}

	out, _ := time.Parse(time.RFC3339, "2026-01-01T22:00:00Z")
	res = DryRun(context.Background(), rs, ToolCallShape{
		TenantID: "acme", Server: "x", Tool: "x.y", Now: out,
	})
	if res.FinalAction.Allow || len(res.MatchedRules) != 0 {
		t.Errorf("expected no match outside hours, got %+v", res)
	}
}

func TestDryRun_RequireApprovalSticks(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "require-approval", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{Tools: []string{"x.y"}}},
			Actions:    Actions{RequireApproval: true}},
		{ID: "allow", Priority: 50, Enabled: true,
			Conditions: Conditions{Match: Match{}},
			Actions:    Actions{Allow: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "x", Tool: "x.y"})
	if !res.FinalAction.RequireApproval {
		t.Errorf("expected approval requirement to stick, got %+v", res.FinalAction)
	}
}

func TestDryRun_ArgsExpr(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "deny-prod", Priority: 1, Enabled: true,
			Conditions: Conditions{Match: Match{
				Tools:    []string{"x.y"},
				ArgsExpr: "env=prod",
			}},
			Actions: Actions{Deny: true}},
	}}
	hit := DryRun(context.Background(), rs, ToolCallShape{
		Server: "x", Tool: "x.y", Args: map[string]any{"env": "prod"},
	})
	if !hit.FinalAction.Deny {
		t.Errorf("expected deny on env=prod, got %+v", hit)
	}
	miss := DryRun(context.Background(), rs, ToolCallShape{
		Server: "x", Tool: "x.y", Args: map[string]any{"env": "staging"},
	})
	if miss.FinalAction.Deny {
		t.Errorf("expected no deny on staging, got %+v", miss)
	}
}
