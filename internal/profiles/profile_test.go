package profiles

import "testing"

func restrictive() *Profile {
	return &Profile{
		TenantID:            "t1",
		ID:                  "ap_1",
		AllowedMCPServers:   []string{"github", "jira"},
		AllowedTools:        []string{"github.list_issues"},
		AllowedSkills:       []string{"code-review"},
		AllowedModelAliases: []string{"gpt-4o"},
	}
}

func TestDefaultProfile_AllowsEverything(t *testing.T) {
	p := DefaultProfile("t1")
	if !p.IsDefault {
		t.Fatal("default profile must be marked IsDefault")
	}
	if !p.AllowsServer("anything") || !p.AllowsTool("x.y") || !p.AllowsSkill("z") || !p.AllowsAlias("any-model") {
		t.Fatal("default profile must allow everything")
	}
}

func TestNilProfile_AllowsEverything(t *testing.T) {
	var p *Profile
	if !p.AllowsServer("x") || !p.AllowsTool("x.y") || !p.AllowsSkill("z") || !p.AllowsAlias("m") {
		t.Fatal("a nil profile must allow everything (no middleware ran)")
	}
}

func TestAllowsServer(t *testing.T) {
	p := restrictive()
	if !p.AllowsServer("github") || !p.AllowsServer("jira") {
		t.Error("allowed servers rejected")
	}
	if p.AllowsServer("slack") {
		t.Error("disallowed server allowed")
	}
}

func TestAllowsTool_RespectsToolAllowlist(t *testing.T) {
	p := restrictive()
	if !p.AllowsTool("github.list_issues") {
		t.Error("explicitly allowed tool rejected")
	}
	// Same server, but not in the tool allowlist.
	if p.AllowsTool("github.delete_repo") {
		t.Error("tool not in the allowlist was allowed")
	}
	// Allowed server, but the tool's server (jira) has no tool entries → with a
	// non-empty AllowedTools, only listed tools pass, so jira.* is rejected.
	if p.AllowsTool("jira.create") {
		t.Error("tool on a server with no allowlist entry was allowed under a non-empty AllowedTools")
	}
	// Disallowed server.
	if p.AllowsTool("slack.post") {
		t.Error("tool on a disallowed server was allowed")
	}
	// Non-namespaced.
	if p.AllowsTool("notnamespaced") {
		t.Error("non-namespaced tool was allowed under a restrictive profile")
	}
}

func TestAllowsTool_EmptyToolAllowlistAllowsAllInServer(t *testing.T) {
	p := &Profile{AllowedMCPServers: []string{"github"}} // AllowedTools empty
	if !p.AllowsTool("github.list_issues") || !p.AllowsTool("github.delete_repo") {
		t.Error("empty AllowedTools should allow all tools in the allowed server")
	}
	if p.AllowsTool("jira.create") {
		t.Error("tool on a disallowed server was allowed")
	}
}

func TestAllowsSkillAndAlias(t *testing.T) {
	p := restrictive()
	if !p.AllowsSkill("code-review") || p.AllowsSkill("triage") {
		t.Error("skill allowlist wrong")
	}
	if !p.AllowsAlias("gpt-4o") || p.AllowsAlias("claude-3-5-sonnet") {
		t.Error("alias allowlist wrong")
	}
}
