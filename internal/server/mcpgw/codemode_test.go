package mcpgw

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/catalog"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// --- opt-in parsing ---------------------------------------------------------

func TestExtractCodeModeOpts_Enabled(t *testing.T) {
	exp := map[string]json.RawMessage{
		"portico": json.RawMessage(`{"code_mode":{"enabled":true,"binding_level":"tool","max_tool_calls":5}}`),
	}
	opts := extractCodeModeOpts(exp)
	if opts == nil {
		t.Fatal("expected opts, got nil")
	}
	if opts.BindingLevel != catalog.BindingTool {
		t.Errorf("binding level = %q, want tool", opts.BindingLevel)
	}
	if opts.MaxToolCalls != 5 {
		t.Errorf("max tool calls = %d, want 5", opts.MaxToolCalls)
	}
}

func TestExtractCodeModeOpts_DefaultsAndCoexistWithListChanged(t *testing.T) {
	exp := map[string]json.RawMessage{
		"portico": json.RawMessage(`{"listChanged":"live","code_mode":{"enabled":true}}`),
	}
	opts := extractCodeModeOpts(exp)
	if opts == nil {
		t.Fatal("expected opts")
	}
	if opts.BindingLevel != catalog.BindingServer {
		t.Errorf("default binding level = %q, want server", opts.BindingLevel)
	}
	if opts.MaxToolCalls != defaultCodeModeMaxToolCalls {
		t.Errorf("default max tool calls = %d, want %d", opts.MaxToolCalls, defaultCodeModeMaxToolCalls)
	}
}

func TestExtractCodeModeOpts_DisabledOrAbsent(t *testing.T) {
	cases := map[string]map[string]json.RawMessage{
		"absent":            {},
		"no_code_mode":      {"portico": json.RawMessage(`{"listChanged":"live"}`)},
		"explicit_disabled": {"portico": json.RawMessage(`{"code_mode":{"enabled":false}}`)},
		"malformed":         {"portico": json.RawMessage(`{not json`)},
	}
	for name, exp := range cases {
		t.Run(name, func(t *testing.T) {
			if opts := extractCodeModeOpts(exp); opts != nil {
				t.Errorf("expected nil opts, got %+v", opts)
			}
		})
	}
}

func TestExtractCodeModeOpts_UnknownBindingLevelFallsBackToServer(t *testing.T) {
	exp := map[string]json.RawMessage{
		"portico": json.RawMessage(`{"code_mode":{"enabled":true,"binding_level":"galaxy"}}`),
	}
	opts := extractCodeModeOpts(exp)
	if opts == nil || opts.BindingLevel != catalog.BindingServer {
		t.Fatalf("unknown level should fall back to server, got %+v", opts)
	}
}

// --- meta-tool advertisement ------------------------------------------------

func TestCodeModeMetaTools_AreReservedNamespaceWithSchemas(t *testing.T) {
	tools := codeModeMetaTools()
	if len(tools) != 4 {
		t.Fatalf("want 4 meta-tools, got %d", len(tools))
	}
	for _, tl := range tools {
		if !strings.HasPrefix(tl.Name, "mcp.") {
			t.Errorf("meta-tool %q not under mcp.* namespace", tl.Name)
		}
		if !isCodeModeMetaTool(tl.Name) {
			t.Errorf("isCodeModeMetaTool(%q) = false", tl.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(tl.InputSchema, &schema); err != nil {
			t.Errorf("meta-tool %q has invalid input schema: %v", tl.Name, err)
		}
	}
	if isCodeModeMetaTool("github.list_issues") {
		t.Error("namespaced tool wrongly classified as meta-tool")
	}
}

// --- handlers (white-box, seeded snapshot) ----------------------------------

// seededBinder returns a SnapshotBinder pre-bound to snap for sessionID. The
// sentinel non-nil service is never dereferenced because Get short-circuits on
// the existing binding before it would call service.Create.
func seededBinder(sessionID string, snap *snapshots.Snapshot) *SnapshotBinder {
	b := NewSnapshotBinder(&snapshots.Service{})
	b.bindings[sessionID] = snap
	return b
}

func codeModeSnapshot() *snapshots.Snapshot {
	return &snapshots.Snapshot{
		ID:       "snap-cm",
		TenantID: "tenant-a",
		Tools: []snapshots.ToolInfo{
			{
				NamespacedName:   "github.list_issues",
				ServerID:         "github",
				Description:      "List issues",
				InputSchema:      json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}},"required":["repo"]}`),
				RiskClass:        "read",
				RequiresApproval: false,
			},
			{
				NamespacedName:   "github.delete_repo",
				ServerID:         "github",
				Description:      "Delete a repo",
				InputSchema:      json.RawMessage(`{"type":"object","properties":{"repo":{"type":"string"}},"required":["repo"]}`),
				RiskClass:        "destructive",
				RequiresApproval: true,
			},
		},
	}
}

func codeModeDispatcher(snap *snapshots.Snapshot, sessID string) *Dispatcher {
	d := NewDispatcher(nil, nil)
	if snap != nil {
		d.SetSnapshotBinder(seededBinder(sessID, snap))
	}
	return d
}

func codeModeSession(id string) *Session {
	s := newSession(id, "tenant-a", "user-1", "")
	s.CodeMode = &CodeModeOpts{BindingLevel: catalog.BindingServer, MaxToolCalls: 20}
	return s
}

func TestMetaListToolFiles_ReturnsProjectedPaths(t *testing.T) {
	sess := codeModeSession("s1")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)

	body, perr := d.handleCodeModeMetaTool(context.Background(), sess, protocol.CallToolParams{Name: metaListToolFiles})
	if perr != nil {
		t.Fatalf("unexpected error: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	var sc struct {
		Files []string `json:"files"`
	}
	mustJSON(t, res.StructuredContent, &sc)
	if !containsStr(sc.Files, "servers/github.pyi") || !containsStr(sc.Files, "index.md") {
		t.Errorf("listToolFiles missing expected paths: %v", sc.Files)
	}
}

func TestMetaListToolFiles_EmptyProjectionWhenNoSnapshot(t *testing.T) {
	sess := codeModeSession("s-empty")
	d := codeModeDispatcher(nil, sess.ID) // no binder → empty projection
	body, perr := d.handleCodeModeMetaTool(context.Background(), sess, protocol.CallToolParams{Name: metaListToolFiles})
	if perr != nil {
		t.Fatalf("unexpected error: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	var sc struct {
		Files []string `json:"files"`
	}
	mustJSON(t, res.StructuredContent, &sc)
	if !containsStr(sc.Files, "index.md") {
		t.Errorf("empty projection should still list index.md: %v", sc.Files)
	}
}

func TestMetaReadToolFile_FoundAndNotFound(t *testing.T) {
	sess := codeModeSession("s2")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)

	body, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaReadToolFile, Arguments: json.RawMessage(`{"path":"servers/github.pyi"}`)})
	if perr != nil {
		t.Fatalf("unexpected error: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	if !strings.Contains(string(res.StructuredContent), "def list_issues") {
		t.Errorf("readToolFile did not return stub content: %s", res.StructuredContent)
	}

	_, perr = d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaReadToolFile, Arguments: json.RawMessage(`{"path":"servers/nope.pyi"}`)})
	if perr == nil || perr.Code != protocol.ErrInvalidParams {
		t.Errorf("expected invalid params for unknown path, got %v", perr)
	}
}

func TestMetaReadToolFile_MissingPathRejected(t *testing.T) {
	sess := codeModeSession("s3")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaReadToolFile, Arguments: json.RawMessage(`{}`)})
	if perr == nil || perr.Code != protocol.ErrInvalidParams {
		t.Errorf("expected invalid params for missing path, got %v", perr)
	}
}

func TestMetaGetToolDocs_FullSurface(t *testing.T) {
	sess := codeModeSession("s4")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	body, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaGetToolDocs, Arguments: json.RawMessage(`{"tools":["github.delete_repo","github.unknown"]}`)})
	if perr != nil {
		t.Fatalf("unexpected error: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	var sc struct {
		Docs []toolDoc `json:"docs"`
	}
	mustJSON(t, res.StructuredContent, &sc)
	if len(sc.Docs) != 2 {
		t.Fatalf("want 2 docs, got %d", len(sc.Docs))
	}
	del := sc.Docs[0]
	if !del.Found || del.RiskClass != "destructive" || !del.RequiresApproval {
		t.Errorf("delete_repo docs wrong: %+v", del)
	}
	if sc.Docs[1].Found {
		t.Errorf("unknown tool should be marked not found: %+v", sc.Docs[1])
	}
}

func TestMetaGetToolDocs_EmptyToolsRejected(t *testing.T) {
	sess := codeModeSession("s5")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaGetToolDocs, Arguments: json.RawMessage(`{"tools":[]}`)})
	if perr == nil || perr.Code != protocol.ErrInvalidParams {
		t.Errorf("expected invalid params for empty tools, got %v", perr)
	}
}

// TestCodeMode_PerSessionSnapshotIsolation proves a session only ever sees its
// own snapshot's projection — the structural basis for per-tenant isolation
// (acceptance #10): tenant identity selects the snapshot, never the arguments.
func TestCodeMode_PerSessionSnapshotIsolation(t *testing.T) {
	snapA := &snapshots.Snapshot{ID: "snap-a", TenantID: "a", Tools: []snapshots.ToolInfo{
		{NamespacedName: "alpha.only", ServerID: "alpha", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	snapB := &snapshots.Snapshot{ID: "snap-b", TenantID: "b", Tools: []snapshots.ToolInfo{
		{NamespacedName: "bravo.only", ServerID: "bravo", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
	binder := NewSnapshotBinder(&snapshots.Service{})
	binder.bindings["sa"] = snapA
	binder.bindings["sb"] = snapB
	d := NewDispatcher(nil, nil)
	d.SetSnapshotBinder(binder)

	sessA := codeModeSession("sa")
	sessA.TenantID = "a"
	sessB := codeModeSession("sb")
	sessB.TenantID = "b"

	filesA := listFiles(t, d, sessA)
	filesB := listFiles(t, d, sessB)

	if !containsStr(filesA, "servers/alpha.pyi") || containsStr(filesA, "servers/bravo.pyi") {
		t.Errorf("tenant A leaked or missing files: %v", filesA)
	}
	if !containsStr(filesB, "servers/bravo.pyi") || containsStr(filesB, "servers/alpha.pyi") {
		t.Errorf("tenant B leaked or missing files: %v", filesB)
	}
}

func listFiles(t *testing.T, d *Dispatcher, sess *Session) []string {
	t.Helper()
	body, perr := d.handleCodeModeMetaTool(context.Background(), sess, protocol.CallToolParams{Name: metaListToolFiles})
	if perr != nil {
		t.Fatalf("listToolFiles: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	var sc struct {
		Files []string `json:"files"`
	}
	mustJSON(t, res.StructuredContent, &sc)
	return sc.Files
}

// --- executeToolCode --------------------------------------------------------

func execCode(t *testing.T, d *Dispatcher, sess *Session, code string) (json.RawMessage, *protocol.Error) {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"code": code})
	return d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaExecuteToolCode, Arguments: args})
}

func TestExecuteToolCode_PureComputeHappyPath(t *testing.T) {
	sess := codeModeSession("e1")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	body, perr := execCode(t, d, sess, "result = 1 + 2 + 3")
	if perr != nil {
		t.Fatalf("unexpected error: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	var sc struct {
		Result    json.RawMessage `json:"result"`
		ToolCalls int             `json:"tool_calls"`
	}
	mustJSON(t, res.StructuredContent, &sc)
	if string(sc.Result) != "6" {
		t.Errorf("result = %s, want 6", sc.Result)
	}
	if sc.ToolCalls != 0 {
		t.Errorf("tool_calls = %d, want 0", sc.ToolCalls)
	}
}

func TestExecuteToolCode_UnsafeSnippetRejected(t *testing.T) {
	sess := codeModeSession("e2")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	_, perr := execCode(t, d, sess, `load("os", "system")
result = 1`)
	if perr == nil || perr.Code != protocol.ErrCodeModeUnsafe {
		t.Fatalf("want ErrCodeModeUnsafe, got %v", perr)
	}
	if !strings.Contains(string(perr.Data), "code_mode.unsafe_call") {
		t.Errorf("error data should carry the code_mode.* reason: %s", perr.Data)
	}
}

func TestExecuteToolCode_StepBudgetMapsToBudgetError(t *testing.T) {
	sess := codeModeSession("e3")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	_, perr := execCode(t, d, sess, `
total = 0
for i in range(100000000):
    total = total + i
result = total`)
	if perr == nil || perr.Code != protocol.ErrCodeModeBudget {
		t.Fatalf("want ErrCodeModeBudget, got %v", perr)
	}
}

// A tool call from inside the sandbox routes through dispatchToolCall — proven
// here by the fail-closed path: with no southbound manager configured, the
// shared dispatch path returns upstream-unavailable, surfaced as a tool error.
// (The happy-path envelope equality is the integration test.)
func TestExecuteToolCode_ToolCallRoutesThroughDispatcher(t *testing.T) {
	sess := codeModeSession("e4")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID) // NewDispatcher(nil,...) → manager nil
	_, perr := execCode(t, d, sess, `result = github.list_issues(repo="owner/r")`)
	if perr == nil || perr.Code != protocol.ErrCodeModeExecution {
		t.Fatalf("want ErrCodeModeExecution (tool error via dispatcher), got %v", perr)
	}
}

func TestExecuteToolCode_EmptyCodeRejected(t *testing.T) {
	sess := codeModeSession("e5")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	_, perr := execCode(t, d, sess, "")
	if perr == nil || perr.Code != protocol.ErrInvalidParams {
		t.Fatalf("want ErrInvalidParams for empty code, got %v", perr)
	}
}

func TestExecuteToolCode_ResultRedacted(t *testing.T) {
	sess := codeModeSession("e6")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	// The default redactor scrubs values under sensitive keys (api_key, token,
	// …) by key name — a benign value is used here so no real-looking secret is
	// committed (gitleaks-safe) while still exercising the redaction path.
	const sentinel = "dummy-value-must-be-redacted"
	body, perr := execCode(t, d, sess, `result = {"api_key": "`+sentinel+`"}`)
	if perr != nil {
		t.Fatalf("unexpected error: %v", perr)
	}
	var res protocol.CallToolResult
	mustJSON(t, body, &res)
	if strings.Contains(string(res.StructuredContent), sentinel) {
		t.Errorf("result leaked the sensitive value: %s", res.StructuredContent)
	}
	if !strings.Contains(string(res.StructuredContent), "REDACTED") {
		t.Errorf("expected redaction marker in result: %s", res.StructuredContent)
	}
}

// --- helpers ----------------------------------------------------------------

func mustJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
