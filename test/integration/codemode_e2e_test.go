package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/profiles"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"

	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// recordingEmitter captures every audit event so a test can compare the
// envelope a direct tools/call produced against the one an in-sandbox Code Mode
// call produced (acceptance #8).
type recordingEmitter struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingEmitter) Emit(_ context.Context, e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// toolCallTypes returns, in order, the audit event types emitted for a specific
// tool within a specific session.
func (r *recordingEmitter) toolCallTypes(sessionID, tool string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		if e.SessionID != sessionID {
			continue
		}
		if t, _ := e.Payload["tool"].(string); t == tool {
			out = append(out, e.Type)
		}
	}
	return out
}

func (r *recordingEmitter) hasType(sessionID, typ string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.SessionID == sessionID && e.Type == typ {
			return true
		}
	}
	return false
}

// startCodeModeServer boots the full gateway with the policy pipeline, a
// recording audit emitter, and the snapshot binder wired — everything an
// in-sandbox tool call must traverse. Mirrors cmd/portico/cmd_serve.go.
func startCodeModeServer(t *testing.T, specs []config.ServerSpec) (*httptest.Server, *recordingEmitter) {
	t.Helper()
	cfg := &config.Config{
		Server:  config.ServerConfig{Bind: "127.0.0.1:0"},
		Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
		Logging: config.LoggingConfig{Level: "error", Format: "json"},
		Servers: specs,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	backend, err := storage.Open(context.Background(), cfg.Storage, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	reg := registry.New(backend.Registry(), logger)
	if err := backend.Tenants().Upsert(context.Background(), &ifaces.Tenant{ID: "dev", DisplayName: "dev", Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	for _, cs := range cfg.Servers {
		if _, err := reg.Upsert(context.Background(), "dev", configToRegistry(cs)); err != nil {
			t.Fatalf("seed registry: %v", err)
		}
	}

	supervisor := process.NewSupervisor(logger, process.NewResolver(nil), reg)
	mgr := southboundmgr.NewManager(reg, supervisor, logger)
	disp := mcpgw.NewDispatcher(mgr, logger)
	sess := mcpgw.NewSessionRegistry()

	recorder := &recordingEmitter{}

	// Policy pipeline (default-allow within tenant) + recording emitter — the
	// same governed chain a direct tools/call runs.
	engine := policy.New(mgr, nil, nil, nil, policy.EngineConfig{
		DefaultRiskClass: policy.RiskRead, // read => no approval needed
		Logger:           logger,
	})
	pipeline := mcpgw.NewPolicyPipeline(mcpgw.PipelineConfig{
		Engine:   engine,
		Emitter:  recorder,
		Registry: reg,
		Logger:   logger,
	})
	disp.SetPolicyPipeline(pipeline)
	disp.SetAuditEmitter(recorder)

	// Snapshot binder so a Code Mode session can project the catalog into stubs
	// and bind tools.
	probe := mcpgw.NewSnapshotProbe(mgr, reg, nil, nil, engine, nil)
	svc := snapshots.NewService(snapshots.NewStorageAdapter(backend.Snapshots()), probe, recorder, logger)
	disp.SetSnapshotBinder(mcpgw.NewSnapshotBinder(svc))

	appsReg := apps.New(apps.DefaultCSP())
	resourceAgg := mcpgw.NewResourceAggregator(mgr, appsReg, mcpgw.DefaultResourceLimits(), logger)
	promptAgg := mcpgw.NewPromptAggregator(mgr, resourceAgg, logger)
	listChangedMux := mcpgw.NewListChangedMux(sess, resourceAgg, mcpgw.ModeStable, logger)
	disp.SetAggregators(resourceAgg, promptAgg, listChangedMux)
	sess.OnClose(disp.InvalidateSession)

	t.Cleanup(func() {
		sess.CloseAll()
		_ = mgr.CloseAll(context.Background())
		reg.CloseAll()
	})

	handler := api.NewRouter(api.Deps{
		Logger:          logger,
		DevMode:         true,
		DevTenant:       "dev",
		Tenants:         backend.Tenants(),
		Audit:           backend.Audit(),
		Sessions:        sess,
		Dispatcher:      disp,
		Manager:         mgr,
		Registry:        reg,
		Apps:            appsReg,
		AgentProfiles:   backend.AgentProfiles(),
		ProfileResolver: profiles.NewResolver(backend.AgentProfiles(), 0, 0),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, recorder
}

func codeModeInitParams() protocol.InitializeParams {
	return protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities: protocol.ClientCapabilities{
			Experimental: map[string]json.RawMessage{
				"portico": json.RawMessage(`{"code_mode":{"enabled":true}}`),
			},
		},
		ClientInfo: protocol.Implementation{Name: "test", Version: "0"},
	}
}

func mockSpec(t *testing.T) []config.ServerSpec {
	return []config.ServerSpec{{
		ID:        "mock",
		Transport: "stdio",
		Stdio:     &config.StdioSpec{Command: e2eMockBinary(t)},
	}}
}

// TestE2E_CodeMode_OptInAndHappyPath: a Code Mode session sees the meta-tools,
// and executeToolCode runs a snippet that calls a real tool and returns its
// result.
func TestE2E_CodeMode_OptInAndHappyPath(t *testing.T) {
	srv, _ := startCodeModeServer(t, mockSpec(t))

	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, codeModeInitParams()))
	if sid == "" {
		t.Fatal("no session id")
	}

	// tools/list shows the meta-tools, not the namespaced catalog.
	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsList, struct{}{}))
	var list protocol.ListToolsResult
	if err := json.Unmarshal(listResp.Result, &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range list.Tools {
		names[tl.Name] = true
	}
	if !names["mcp.executeToolCode"] || names["mock.echo"] {
		t.Fatalf("code mode session should see meta-tools only, got %v", names)
	}

	// executeToolCode calling the real tool.
	args, _ := json.Marshal(map[string]string{"code": `result = mock.echo(message="hi from sandbox")`})
	resp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mcp.executeToolCode", Arguments: args,
	}))
	if resp.Error != nil {
		t.Fatalf("executeToolCode error: %+v", resp.Error)
	}
	var res protocol.CallToolResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatal(err)
	}
	var sc struct {
		Result    json.RawMessage `json:"result"`
		ToolCalls int             `json:"tool_calls"`
	}
	if err := json.Unmarshal(res.StructuredContent, &sc); err != nil {
		t.Fatal(err)
	}
	if sc.ToolCalls != 1 {
		t.Errorf("tool_calls = %d, want 1", sc.ToolCalls)
	}
	// The echo tool returns its message in a text content block; the sandbox
	// result is the raw CallToolResult, so the message must survive.
	if !contains(string(sc.Result), "hi from sandbox") {
		t.Errorf("sandbox result missing tool output: %s", sc.Result)
	}
}

// TestE2E_CodeMode_AuditEnvelope_Complete is the acceptance-#8 proof: a tool
// dispatched from inside the sandbox produces the SAME ordered audit envelope as
// the identical direct tools/call.
func TestE2E_CodeMode_AuditEnvelope_Complete(t *testing.T) {
	srv, rec := startCodeModeServer(t, mockSpec(t))

	// Direct path (plain session).
	_, directSid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "direct", Version: "0"},
	}))
	dargs, _ := json.Marshal(map[string]string{"message": "x"})
	dResp, _ := rpcPost(t, srv.URL, directSid, newReq(2, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mock.echo", Arguments: dargs,
	}))
	if dResp.Error != nil {
		t.Fatalf("direct call error: %+v", dResp.Error)
	}

	// Sandbox path (code mode session).
	_, sandboxSid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, codeModeInitParams()))
	sargs, _ := json.Marshal(map[string]string{"code": `result = mock.echo(message="x")`})
	sResp, _ := rpcPost(t, srv.URL, sandboxSid, newReq(2, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mcp.executeToolCode", Arguments: sargs,
	}))
	if sResp.Error != nil {
		t.Fatalf("sandbox call error: %+v", sResp.Error)
	}

	directTypes := rec.toolCallTypes(directSid, "mock.echo")
	sandboxTypes := rec.toolCallTypes(sandboxSid, "mock.echo")

	if len(directTypes) == 0 {
		t.Fatal("direct call produced no audit events for mock.echo")
	}
	if !equalStrings(directTypes, sandboxTypes) {
		t.Fatalf("envelope mismatch:\n direct  = %v\n sandbox = %v", directTypes, sandboxTypes)
	}
	// The sandbox call must additionally mark the enclosing execution.
	if !rec.hasType(sandboxSid, audit.EventCodeModeExecStarted) || !rec.hasType(sandboxSid, audit.EventCodeModeExecCompleted) {
		t.Error("sandbox execution did not emit code_mode.execution_started/completed")
	}
	// Adversarial: the in-sandbox call must have gone through the start/complete
	// envelope — proving it could not skip the dispatch path.
	if !rec.hasType(sandboxSid, audit.EventToolCallStart) || !rec.hasType(sandboxSid, audit.EventToolCallComplete) {
		t.Error("in-sandbox tool call did not traverse tool_call.start/complete (envelope skipped)")
	}
}

// TestE2E_CodeMode_UnboundToolCannotReachDispatch: a snippet referencing a
// server not in the snapshot is rejected by the static gate and never reaches
// the dispatcher — no tool_call audit event is produced.
func TestE2E_CodeMode_UnboundToolCannotReachDispatch(t *testing.T) {
	srv, rec := startCodeModeServer(t, mockSpec(t))
	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, codeModeInitParams()))
	args, _ := json.Marshal(map[string]string{"code": `result = secretserver.exfiltrate(all=True)`})
	resp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mcp.executeToolCode", Arguments: args,
	}))
	if resp.Error == nil {
		t.Fatal("expected an error for an unbound server reference")
	}
	if len(rec.toolCallTypes(sid, "secretserver.exfiltrate")) != 0 {
		t.Error("unbound tool produced a dispatch audit event — it reached the dispatcher")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
