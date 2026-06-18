package policy

import (
	"context"
	"testing"
)

func githubProfile() *ProfileView {
	return &ProfileView{
		ID:      "ap_1",
		Name:    "engineering",
		Servers: []string{"github", "jira"},
		Aliases: []string{"gpt-4o"},
	}
}

func TestMatcher_ProfileByIDOrName(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "r1", Enabled: true, Conditions: Conditions{Match: Match{Profiles: []string{"ap_1"}}}, Actions: Actions{Deny: true}},
	}}
	// Matches by id.
	res := DryRun(context.Background(), rs, ToolCallShape{Tool: "github.x", Profile: githubProfile()})
	if !res.FinalAction.Deny {
		t.Fatal("rule should match profile by id and deny")
	}
	// Matches by name.
	rs.Rules[0].Conditions.Match.Profiles = []string{"engineering"}
	res = DryRun(context.Background(), rs, ToolCallShape{Tool: "github.x", Profile: githubProfile()})
	if !res.FinalAction.Deny {
		t.Fatal("rule should match profile by name")
	}
	// No profile context → rule does not fire.
	res = DryRun(context.Background(), rs, ToolCallShape{Tool: "github.x"})
	if res.FinalAction.Deny {
		t.Fatal("profile matcher must not fire without a profile in context")
	}
	// Different profile → no match.
	res = DryRun(context.Background(), rs, ToolCallShape{Tool: "github.x", Profile: &ProfileView{ID: "ap_2", Name: "other"}})
	if res.FinalAction.Deny {
		t.Fatal("rule must not match a different profile")
	}
}

func TestMatcher_ProfileIncludesServer(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "r1", Enabled: true, Conditions: Conditions{Match: Match{ProfileIncludesServer: "github"}}, Actions: Actions{RequireApproval: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Tool: "github.x", Profile: githubProfile()})
	if !res.FinalAction.RequireApproval {
		t.Fatal("includes_server=github should match (profile allows github)")
	}
	// Profile without that server → no match.
	res = DryRun(context.Background(), rs, ToolCallShape{Tool: "x", Profile: &ProfileView{ID: "ap_3", Servers: []string{"slack"}}})
	if res.FinalAction.RequireApproval {
		t.Fatal("includes_server must not match a profile lacking the server")
	}
}

func TestMatcher_ProfileIncludesAlias(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "r1", Enabled: true, Conditions: Conditions{Match: Match{ProfileIncludesAlias: "gpt-4o"}}, Actions: Actions{Deny: true}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Tool: "x", Profile: githubProfile()})
	if !res.FinalAction.Deny {
		t.Fatal("includes_alias=gpt-4o should match")
	}
	res = DryRun(context.Background(), rs, ToolCallShape{Tool: "x", Profile: &ProfileView{Aliases: []string{"claude-3-5-sonnet"}}})
	if res.FinalAction.Deny {
		t.Fatal("includes_alias must not match a profile lacking the alias")
	}
}

func TestAction_RequireProfileMembership_Allow(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "r1", Enabled: true, Conditions: Conditions{Match: Match{Tools: []string{"github.*"}}},
			Actions: Actions{RequireProfileMembership: []string{"ap_1", "ap_9"}}},
	}}
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "github", Tool: "github.x", Profile: githubProfile()})
	if res.FinalAction.Deny {
		t.Fatal("member profile must be allowed (no deny)")
	}
	if len(res.MatchedRules) != 1 {
		t.Fatalf("rule should have matched: %+v", res.MatchedRules)
	}
}

func TestAction_RequireProfileMembership_Deny(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "r1", Enabled: true, Conditions: Conditions{Match: Match{Tools: []string{"github.*"}}},
			Actions: Actions{RequireProfileMembership: []string{"ap_9"}}},
	}}
	// Non-member profile → deny.
	res := DryRun(context.Background(), rs, ToolCallShape{Server: "github", Tool: "github.x", Profile: githubProfile()})
	if !res.FinalAction.Deny {
		t.Fatal("non-member profile must be denied by require_profile_membership")
	}
	// No profile context → also denied (fail closed).
	res = DryRun(context.Background(), rs, ToolCallShape{Server: "github", Tool: "github.x"})
	if !res.FinalAction.Deny {
		t.Fatal("absent profile must be denied by require_profile_membership")
	}
}

func TestValidate_RequireProfileMembership_MutuallyExclusive(t *testing.T) {
	r := Rule{ID: "r1", Actions: Actions{Allow: true, RequireProfileMembership: []string{"ap_1"}}}
	if err := Validate(r); err == nil {
		t.Fatal("allow + require_profile_membership must be rejected as mutually exclusive")
	}
	// Sole membership action is valid.
	if err := Validate(Rule{ID: "r2", Actions: Actions{RequireProfileMembership: []string{"ap_1"}}}); err != nil {
		t.Fatalf("sole require_profile_membership should validate: %v", err)
	}
}

func TestProfileMatchers_RoundTripCanonical(t *testing.T) {
	// New Match/Actions fields must survive the encode/decode the SQL store uses.
	in := Conditions{Match: Match{Profiles: []string{"ap_1"}, ProfileIncludesServer: "github", ProfileIncludesAlias: "gpt-4o"}}
	b, err := EncodeConditions(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeConditions(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Match.Profiles) != 1 || got.Match.ProfileIncludesServer != "github" || got.Match.ProfileIncludesAlias != "gpt-4o" {
		t.Fatalf("profile matchers did not round-trip: %+v", got.Match)
	}
	ab, err := EncodeActions(Actions{RequireProfileMembership: []string{"ap_1", "ap_2"}})
	if err != nil {
		t.Fatal(err)
	}
	ga, err := DecodeActions(ab)
	if err != nil {
		t.Fatal(err)
	}
	if len(ga.RequireProfileMembership) != 2 {
		t.Fatalf("require_profile_membership did not round-trip: %+v", ga)
	}
}
