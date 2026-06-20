package profiles

import (
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func restrictive() *Profile {
	return &Profile{
		TenantID:            "t1",
		ID:                  "ap_1",
		AllowedMCPServers:   []string{"github", "jira"},
		AllowedTools:        []string{"github.list_issues"},
		AllowedSkills:       []string{"code-review"},
		AllowedModelAliases: []string{"gpt-4o"},
		AllowedA2APeers:     []string{"research-agent", "reviewer"},
		AllowedA2ATasks:     []string{"research-agent.code-review"},
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
	if !p.AllowsA2APeer("any-peer") || !p.AllowsA2ATask("any-peer.any-task") {
		t.Fatal("default profile must allow every A2A peer and task")
	}
}

func TestNilProfile_AllowsEverything(t *testing.T) {
	var p *Profile
	if !p.AllowsServer("x") || !p.AllowsTool("x.y") || !p.AllowsSkill("z") || !p.AllowsAlias("m") {
		t.Fatal("a nil profile must allow everything (no middleware ran)")
	}
	if !p.AllowsA2APeer("peer") || !p.AllowsA2ATask("peer.task") {
		t.Fatal("a nil profile must allow every A2A peer and task")
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

func TestAllowsA2APeer(t *testing.T) {
	p := restrictive()
	if !p.AllowsA2APeer("research-agent") || !p.AllowsA2APeer("reviewer") {
		t.Error("allowed peers rejected")
	}
	if p.AllowsA2APeer("unknown-agent") {
		t.Error("disallowed peer allowed")
	}

	// A profile with an empty AllowedA2APeers allows NOTHING (membership
	// semantics, mirroring AllowsServer). Restricting peers via empty
	// allowlist is a deliberate back-compat edge case, not "allow all".
	empty := &Profile{AllowedA2ATasks: []string{"x.y"}}
	if empty.AllowsA2APeer("anything") {
		t.Error("empty AllowedA2APeers should not match any peer")
	}
}

func TestAllowsA2ATask(t *testing.T) {
	// Peer allowed + task allowlist empty → all tasks of allowed peers pass.
	peerOnly := &Profile{AllowedA2APeers: []string{"research-agent"}}
	if !peerOnly.AllowsA2ATask("research-agent.code-review") || !peerOnly.AllowsA2ATask("research-agent.another") {
		t.Error("peer-only profile must allow every task of allowed peers")
	}
	if peerOnly.AllowsA2ATask("other.task") {
		t.Error("task on a disallowed peer was allowed")
	}

	// Peer allowed + task listed.
	listedP := restrictive()
	if !listedP.AllowsA2ATask("research-agent.code-review") {
		t.Error("allowed peer + listed task rejected")
	}

	// Peer allowed + task NOT listed (with a non-empty AllowedA2ATasks).
	if listedP.AllowsA2ATask("research-agent.other") {
		t.Error("allowed peer + unlisted task was allowed")
	}

	// Disallowed peer's task — even if the task id happens to be in the list,
	// the peer gate is upstream of the task gate.
	if listedP.AllowsA2ATask("reviewer.code-review") {
		t.Error("disallowed peer's task was allowed")
	}

	// Non-namespaced — a bare peer name isn't a namespaced task id, so the
	// split fails and the gate rejects it under a restrictive profile.
	if listedP.AllowsA2ATask("notnamespaced") {
		t.Error("non-namespaced A2A task was allowed under a restrictive profile")
	}
}

func TestBridgeForMCPTool(t *testing.T) {
	p := &Profile{
		MCPToA2ABridges: []ifaces.MCPToA2ABridge{
			{MCPTool: "github.code-review.run", A2APeer: "research-agent", A2ATask: "code-review"},
		},
	}
	b, ok := p.BridgeForMCPTool("github.code-review.run")
	if !ok || b.A2APeer != "research-agent" || b.A2ATask != "code-review" {
		t.Errorf("BridgeForMCPTool = %+v, ok=%v", b, ok)
	}
	if _, ok := p.BridgeForMCPTool("other.tool"); ok {
		t.Error("unmapped tool reported a bridge")
	}
	var nilP *Profile
	if _, ok := nilP.BridgeForMCPTool("x"); ok {
		t.Error("nil profile reported a bridge")
	}
}

func TestBridgeForA2ATask(t *testing.T) {
	p := &Profile{
		A2AToMCPBridges: []ifaces.A2AToMCPBridge{
			{A2ATask: "billing.refund", MCPTool: "billing.refund"},
		},
	}
	b, ok := p.BridgeForA2ATask("billing.refund")
	if !ok || b.MCPTool != "billing.refund" {
		t.Errorf("BridgeForA2ATask = %+v, ok=%v", b, ok)
	}
	if _, ok := p.BridgeForA2ATask("unknown.task"); ok {
		t.Error("unmapped task reported a bridge")
	}
}
