package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// startCodeModeApprovalServer boots the gateway with an approval flow on the
// fallback path (no elicitation) and the Code Mode continuation store wired —
// the full surface the suspend/resume cycle needs. DefaultRiskClass is
// external_side_effect so every tool call hits the approval gate.
func startCodeModeApprovalServer(t *testing.T) (*httptest.Server, *approval.Flow) {
	t.Helper()
	cfg := &config.Config{
		Server:  config.ServerConfig{Bind: "127.0.0.1:0"},
		Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
		Logging: config.LoggingConfig{Level: "error", Format: "json"},
		Servers: mockSpec(t),
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

	// Approval flow on the fallback path: no Sender + no SessionLookup means
	// HasElicitation is false, so Run returns fallback_required and leaves a
	// pending row an operator resolves out of band.
	flow := approval.New(approval.NewStorageAdapter(backend.Approvals()), nil, nil, recorder, logger)

	engine := policy.New(mgr, nil, nil, nil, policy.EngineConfig{
		DefaultRiskClass: policy.RiskExternalSideEffect, // => requires approval
		Logger:           logger,
	})
	pipeline := mcpgw.NewPolicyPipeline(mcpgw.PipelineConfig{
		Engine:    engine,
		Approvals: flow,
		Emitter:   recorder,
		Registry:  reg,
		Logger:    logger,
	})
	disp.SetPolicyPipeline(pipeline)
	disp.SetAuditEmitter(recorder)
	disp.SetCodeModeStore(backend.CodeMode())

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
		Logger:     logger,
		DevMode:    true,
		DevTenant:  "dev",
		Tenants:    backend.Tenants(),
		Audit:      backend.Audit(),
		Sessions:   sess,
		Dispatcher: disp,
		Manager:    mgr,
		Registry:   reg,
		Apps:       appsReg,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, flow
}

// execResult is the structured payload mcp.executeToolCode returns.
type execResult struct {
	Status            string          `json:"status"`
	ApprovalID        string          `json:"approval_id"`
	ContinuationToken string          `json:"continuation_token"`
	Tool              string          `json:"tool"`
	Result            json.RawMessage `json:"result"`
	ToolCalls         int             `json:"tool_calls"`
}

func execToolCode(t *testing.T, srv *httptest.Server, sid string, id int, args map[string]string) execResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	resp, _ := rpcPost(t, srv.URL, sid, newReq(id, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mcp.executeToolCode", Arguments: raw,
	}))
	if resp.Error != nil {
		t.Fatalf("executeToolCode error: %+v", resp.Error)
	}
	var res protocol.CallToolResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("decode CallToolResult: %v", err)
	}
	var sc execResult
	if err := json.Unmarshal(res.StructuredContent, &sc); err != nil {
		t.Fatalf("decode structured: %v (%s)", err, res.StructuredContent)
	}
	return sc
}

// TestE2E_CodeMode_ApprovalSuspension_AndResume is the acceptance-#9 proof: an
// in-sandbox call to an approval-gated tool suspends with a continuation token;
// after the operator approves, resuming with that token replays deterministically
// and returns the tool's real result. The awaited call traverses the identical
// governed envelope on resume — only the approval gate recognises the grant.
func TestE2E_CodeMode_ApprovalSuspension_AndResume(t *testing.T) {
	srv, flow := startCodeModeApprovalServer(t)
	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, codeModeInitParams()))
	if sid == "" {
		t.Fatal("no session id")
	}

	// First call suspends on the approval-gated tool.
	first := execToolCode(t, srv, sid, 2, map[string]string{
		"code": `result = mock.echo(message="needs approval")`,
	})
	if first.Status != "approval_required" {
		t.Fatalf("status = %q, want approval_required (full: %+v)", first.Status, first)
	}
	if first.ContinuationToken == "" || first.ApprovalID == "" {
		t.Fatalf("suspension missing token/approval id: %+v", first)
	}
	if first.Tool != "mock.echo" {
		t.Errorf("awaited tool = %q, want mock.echo", first.Tool)
	}

	// Operator approves out of band.
	if _, err := flow.ResolveManually(context.Background(), "dev", first.ApprovalID, approval.StatusApproved, "operator"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Resume with the continuation token → the awaited call re-dispatches, the
	// grant is recognised, and the real result comes back.
	resumed := execToolCode(t, srv, sid, 3, map[string]string{
		"continuation_token": first.ContinuationToken,
	})
	if resumed.Status == "approval_required" {
		t.Fatalf("resume re-suspended instead of completing: %+v", resumed)
	}
	if resumed.ToolCalls != 1 {
		t.Errorf("resumed tool_calls = %d, want 1", resumed.ToolCalls)
	}
	if !contains(string(resumed.Result), "needs approval") {
		t.Errorf("resumed result missing tool output: %s", resumed.Result)
	}

	// A second resume with the SAME token must fail closed (double_resume).
	raw, _ := json.Marshal(map[string]string{"continuation_token": first.ContinuationToken})
	resp, _ := rpcPost(t, srv.URL, sid, newReq(4, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mcp.executeToolCode", Arguments: raw,
	}))
	if resp.Error == nil {
		t.Fatal("second resume must error (double_resume), got success")
	}
	if !contains(string(resp.Error.Data), "double_resume") {
		t.Errorf("second resume error should be double_resume, got %s", resp.Error.Data)
	}
}

// TestE2E_CodeMode_ResumeUnknownToken_FailsClosed proves an unknown / forged
// continuation token is rejected (not-found), never executed.
func TestE2E_CodeMode_ResumeUnknownToken_FailsClosed(t *testing.T) {
	srv, _ := startCodeModeApprovalServer(t)
	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, codeModeInitParams()))
	raw, _ := json.Marshal(map[string]string{"continuation_token": "forged-token-aaaaaaaaaaaa"})
	resp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mcp.executeToolCode", Arguments: raw,
	}))
	if resp.Error == nil {
		t.Fatal("unknown continuation token must error")
	}
	if !contains(string(resp.Error.Data), "continuation_not_found") {
		t.Errorf("want continuation_not_found, got %s", resp.Error.Data)
	}
}
