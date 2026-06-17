package catalog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

func sampleSnapshot() *snapshots.Snapshot {
	return &snapshots.Snapshot{
		ID:       "snap-1",
		TenantID: "tenant-a",
		Tools: []snapshots.ToolInfo{
			{
				NamespacedName: "github.list_issues",
				ServerID:       "github",
				Description:    "List issues in a repo",
				InputSchema:    json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"state":{"type":"string"}},"required":["repo"]}`),
			},
			{
				NamespacedName: "github.comment_on",
				ServerID:       "github",
				Description:    "Comment on an issue",
				InputSchema:    json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"},"issue":{"type":"integer"},"body":{"type":"string"}},"required":["repo","issue","body"]}`),
			},
			{
				NamespacedName: "jira.create",
				ServerID:       "jira",
				Description:    "Create a ticket",
				InputSchema:    json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`),
			},
		},
	}
}

func TestProjector_ServerLevel_Deterministic(t *testing.T) {
	snap := sampleSnapshot()
	a := Project(snap, BindingServer)
	b := Project(snap, BindingServer)
	if len(a.Files) != len(b.Files) {
		t.Fatalf("file count differs: %d vs %d", len(a.Files), len(b.Files))
	}
	for path, content := range a.Files {
		if b.Files[path] != content {
			t.Errorf("non-deterministic content for %s", path)
		}
	}
	// Expect: index.md + servers/github.pyi + servers/jira.pyi.
	for _, want := range []string{"index.md", "servers/github.pyi", "servers/jira.pyi"} {
		if _, ok := a.Files[want]; !ok {
			t.Errorf("missing file %s; have %v", want, keys(a.Files))
		}
	}
	if len(a.Files) != 3 {
		t.Errorf("server-level should produce 3 files, got %d: %v", len(a.Files), keys(a.Files))
	}
}

func TestProjector_ServerStub_ContainsSignatures(t *testing.T) {
	a := Project(sampleSnapshot(), BindingServer)
	stub := a.Files["servers/github.pyi"]
	if !strings.Contains(stub, "def list_issues(repo: str") {
		t.Errorf("missing list_issues signature:\n%s", stub)
	}
	if !strings.Contains(stub, "def comment_on(repo: str, issue: int, body: str)") {
		t.Errorf("missing comment_on signature:\n%s", stub)
	}
	if !strings.Contains(stub, `module "github"`) {
		t.Errorf("missing module header:\n%s", stub)
	}
}

func TestProjector_ToolLevel_OneFilePerTool(t *testing.T) {
	a := Project(sampleSnapshot(), BindingTool)
	// server files + per-tool files + index.
	for _, want := range []string{
		"servers/github.pyi",
		"servers/jira.pyi",
		"servers/github/list_issues.pyi",
		"servers/github/comment_on.pyi",
		"servers/jira/create.pyi",
		"index.md",
	} {
		if _, ok := a.Files[want]; !ok {
			t.Errorf("tool-level missing %s; have %v", want, keys(a.Files))
		}
	}
}

func TestProjector_BindingsMapToNamespacedTools(t *testing.T) {
	a := Project(sampleSnapshot(), BindingServer)
	if len(a.Tools) != 3 {
		t.Fatalf("want 3 tool refs, got %d", len(a.Tools))
	}
	got := map[string]ToolRef{}
	for _, r := range a.Tools {
		got[r.Namespaced] = r
	}
	li := got["github.list_issues"]
	if li.Module != "github" || li.Func != "list_issues" {
		t.Errorf("github.list_issues ref = %+v", li)
	}
	jc := got["jira.create"]
	if jc.Module != "jira" || jc.Func != "create" {
		t.Errorf("jira.create ref = %+v", jc)
	}
}

func TestProjector_SanitizesModuleAndFuncNames(t *testing.T) {
	snap := &snapshots.Snapshot{
		ID: "snap-x",
		Tools: []snapshots.ToolInfo{
			{NamespacedName: "my-server.do-thing", ServerID: "my-server", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}
	a := Project(snap, BindingServer)
	if len(a.Tools) != 1 {
		t.Fatalf("want 1 ref, got %d", len(a.Tools))
	}
	ref := a.Tools[0]
	if ref.Module != "my_server" || ref.Func != "do_thing" {
		t.Errorf("sanitized ref = %+v, want my_server.do_thing", ref)
	}
	if ref.Namespaced != "my-server.do-thing" {
		t.Errorf("namespaced name must stay raw: %q", ref.Namespaced)
	}
}

func TestProjector_EmptySnapshot(t *testing.T) {
	a := Project(&snapshots.Snapshot{ID: "empty"}, BindingServer)
	if _, ok := a.Files["index.md"]; !ok {
		t.Errorf("empty snapshot should still have index.md")
	}
	if len(a.Tools) != 0 {
		t.Errorf("empty snapshot should have no tools")
	}
	if !strings.Contains(a.Files["index.md"], "No servers") {
		t.Errorf("empty index should note no servers:\n%s", a.Files["index.md"])
	}
}

func TestProjector_NilSnapshot(t *testing.T) {
	a := Project(nil, BindingServer)
	if _, ok := a.Files["index.md"]; !ok {
		t.Errorf("nil snapshot should produce index.md, not panic")
	}
}

func TestProjector_IndexListsServers(t *testing.T) {
	a := Project(sampleSnapshot(), BindingServer)
	idx := a.Files["index.md"]
	if !strings.Contains(idx, "servers/github.pyi` — 2 tool(s)") {
		t.Errorf("index missing github line:\n%s", idx)
	}
	if !strings.Contains(idx, "servers/jira.pyi` — 1 tool(s)") {
		t.Errorf("index missing jira line:\n%s", idx)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
