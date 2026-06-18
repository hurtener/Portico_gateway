package mcpgw

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

func threeTools() []protocol.Tool {
	return []protocol.Tool{
		{Name: "github.list_issues"},
		{Name: "github.delete_repo"},
		{Name: "jira.create"},
	}
}

func TestFilterToolsByProfile(t *testing.T) {
	tools := threeTools()

	// Restrictive: only github, and within it only list_issues.
	restrictive := &profiles.Profile{AllowedMCPServers: []string{"github"}, AllowedTools: []string{"github.list_issues"}}
	got := filterToolsByProfile(profiles.WithProfile(context.Background(), restrictive), tools)
	if len(got) != 1 || got[0].Name != "github.list_issues" {
		t.Fatalf("restrictive filter wrong: %+v", got)
	}

	// Default profile → unchanged (back-compat).
	if g := filterToolsByProfile(profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1")), tools); len(g) != 3 {
		t.Errorf("default profile must not filter: got %d", len(g))
	}
	// No profile in ctx → unchanged.
	if g := filterToolsByProfile(context.Background(), tools); len(g) != 3 {
		t.Errorf("absent profile must not filter: got %d", len(g))
	}
}

func TestCheckToolAllowedByProfile(t *testing.T) {
	d := NewDispatcher(nil, nil)
	em := &audit.SliceEmitter{}
	d.SetAuditEmitter(em)
	sess := newSession("s1", "t1", "u1", "")
	ctx := profiles.WithProfile(context.Background(), &profiles.Profile{ID: "ap_1", AllowedMCPServers: []string{"github"}})

	// In-surface → allowed (no error, no audit).
	if perr := d.checkToolAllowedByProfile(ctx, sess, "github.list_issues"); perr != nil {
		t.Fatalf("in-surface tool rejected: %v", perr)
	}
	// Out-of-surface → typed violation + audit event.
	perr := d.checkToolAllowedByProfile(ctx, sess, "jira.create")
	if perr == nil || perr.Code != protocol.ErrAgentProfileViolation {
		t.Fatalf("out-of-surface tool not rejected with violation: %v", perr)
	}
	var sawViolation bool
	for _, e := range em.Events() {
		if e.Type == audit.EventAgentProfileViolation {
			sawViolation = true
		}
	}
	if !sawViolation {
		t.Error("expected an agent_profile.violation audit event")
	}

	// Default profile → allowed (back-compat).
	ctxD := profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1"))
	if perr := d.checkToolAllowedByProfile(ctxD, sess, "jira.create"); perr != nil {
		t.Errorf("default profile must allow everything: %v", perr)
	}
}

func TestHandleToolsList_FilteredByProfile(t *testing.T) {
	snap := &snapshots.Snapshot{ID: "snap-1", TenantID: "t1", Tools: []snapshots.ToolInfo{
		{NamespacedName: "github.list_issues", ServerID: "github", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{NamespacedName: "jira.create", ServerID: "jira", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	sess := newSession("s1", "t1", "u1", "")
	d := NewDispatcher(nil, nil)
	d.SetSnapshotBinder(seededBinder(sess.ID, snap))

	ctx := profiles.WithProfile(context.Background(), &profiles.Profile{AllowedMCPServers: []string{"github"}})
	body, perr := d.handleToolsList(ctx, sess, nil)
	if perr != nil {
		t.Fatalf("tools/list: %v", perr)
	}
	var res protocol.ListToolsResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "github.list_issues" {
		t.Fatalf("tools/list not filtered by profile: %+v", res.Tools)
	}

	// Without a profile, both tools are visible (back-compat).
	body2, _ := d.handleToolsList(context.Background(), sess, nil)
	var res2 protocol.ListToolsResult
	_ = json.Unmarshal(body2, &res2)
	if len(res2.Tools) != 2 {
		t.Fatalf("absent profile must show full catalog: %+v", res2.Tools)
	}
}
