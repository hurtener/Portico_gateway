package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"

	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// ----- helpers -----------------------------------------------------------

var (
	e2eOnce sync.Once
	e2eBin  string
	e2eErr  error
)

func e2eMockBinary(t *testing.T) string {
	t.Helper()
	e2eOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mockmcp-e2e-")
		if err != nil {
			e2eErr = err
			return
		}
		bin := filepath.Join(dir, "mockmcp")
		root, err := repoRoot()
		if err != nil {
			e2eErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", bin, "./examples/servers/mock/cmd/mockmcp")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			e2eErr = errors.New(string(out))
			return
		}
		e2eBin = bin
	})
	if e2eErr != nil {
		t.Fatalf("build mockmcp: %v", e2eErr)
	}
	return e2eBin
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// startMcpDevServer boots a dev-mode gateway with the supplied server specs.
func startMcpDevServer(t *testing.T, specs []config.ServerSpec) (*httptest.Server, *southboundmgr.Manager) {
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
	// Seed the dev tenant so subsequent registry inserts satisfy the
	// servers.tenant_id FK constraint. cmd_serve.go relies on the auth
	// middleware to create this lazily; tests that bypass auth must do it
	// explicitly.
	if err := backend.Tenants().Upsert(context.Background(), &ifaces.Tenant{
		ID: "dev", DisplayName: "dev", Plan: "free",
	}); err != nil {
		t.Fatal(err)
	}
	// Seed the test scaffolding's registry from cfg.Servers under the dev
	// tenant so the dispatcher can find them (mirrors cmd_serve.go).
	for _, cs := range cfg.Servers {
		spec := configToRegistry(cs)
		if _, err := reg.Upsert(context.Background(), "dev", spec); err != nil {
			t.Fatalf("seed registry: %v", err)
		}
	}
	supervisor := process.NewSupervisor(logger, process.NewResolver(nil), reg)
	mgr := southboundmgr.NewManager(reg, supervisor, logger)
	disp := mcpgw.NewDispatcher(mgr, logger)
	sess := mcpgw.NewSessionRegistry()

	// Phase 3 wiring — same as cmd/portico/cmd_serve.go.
	appsReg := apps.New(apps.DefaultCSP())
	resourceAgg := mcpgw.NewResourceAggregator(mgr, appsReg, mcpgw.DefaultResourceLimits(), logger)
	promptAgg := mcpgw.NewPromptAggregator(mgr, resourceAgg, logger)
	listChangedMux := mcpgw.NewListChangedMux(sess, resourceAgg, mcpgw.ModeStable, logger)
	disp.SetAggregators(resourceAgg, promptAgg, listChangedMux)
	supervisor.SetNotifSink(func(ctx context.Context, serverID string, n protocol.Notification) {
		listChangedMux.OnDownstream(ctx, serverID, n)
	})
	sess.OnClose(listChangedMux.ForgetSession)

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
	return srv, mgr
}

// rpcPost sends a JSON-RPC body to the gateway and returns the parsed response.
// If sessionID is non-empty it is sent as Mcp-Session-Id; the returned sid is
// the value from the response header (server may rotate / set on initialize).
func rpcPost(t *testing.T, base, sessionID string, req protocol.Request) (resp protocol.Response, sid string) {
	t.Helper()
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	res, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("rpcPost status=%d body=%s", res.StatusCode, raw)
	}
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	sid = res.Header.Get("Mcp-Session-Id")
	return resp, sid
}

// configToRegistry mirrors cmd/portico/cmd_serve.go's translation. Tests
// that build a *config.Config seed registry rows from it directly.
func configToRegistry(c config.ServerSpec) *registry.ServerSpec {
	out := &registry.ServerSpec{
		ID:          c.ID,
		DisplayName: c.DisplayName,
		Transport:   c.Transport,
		RuntimeMode: c.RuntimeMode,
	}
	if c.Stdio != nil {
		out.Stdio = &registry.StdioSpec{
			Command:      c.Stdio.Command,
			Args:         append([]string(nil), c.Stdio.Args...),
			Env:          append([]string(nil), c.Stdio.Env...),
			Cwd:          c.Stdio.Cwd,
			StartTimeout: registry.Duration(c.StartTimeout),
		}
	}
	if c.HTTP != nil {
		out.HTTP = &registry.HTTPSpec{
			URL:        c.HTTP.URL,
			AuthHeader: c.HTTP.AuthHeader,
			Timeout:    registry.Duration(c.HTTP.Timeout),
		}
	}
	return out
}

func newReq(id int, method string, params any) protocol.Request {
	pBody, _ := json.Marshal(params)
	idBytes, _ := json.Marshal(id)
	return protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      idBytes,
		Method:  method,
		Params:  pBody,
	}
}

// ----- tests -------------------------------------------------------------

func TestE2E_StdioDownstream(t *testing.T) {
	bin := e2eMockBinary(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{
			ID:        "mock",
			Transport: "stdio",
			Stdio:     &config.StdioSpec{Command: bin},
		},
	})

	// initialize
	initReq := newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo:      protocol.Implementation{Name: "test", Version: "0"},
	})
	resp, sid := rpcPost(t, srv.URL, "", initReq)
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	if sid == "" {
		t.Fatal("initialize did not return Mcp-Session-Id")
	}

	// tools/list
	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsList, struct{}{}))
	if listResp.Error != nil {
		t.Fatalf("tools/list error: %+v", listResp.Error)
	}
	var listResult protocol.ListToolsResult
	if err := json.Unmarshal(listResp.Result, &listResult); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"mock.echo":   false,
		"mock.add":    false,
		"mock.slow":   false,
		"mock.broken": false,
	}
	for _, tt := range listResult.Tools {
		want[tt.Name] = true
	}
	for n, ok := range want {
		if !ok {
			t.Errorf("missing tool %q in aggregated list", n)
		}
	}

	// tools/call: echo
	args, _ := json.Marshal(map[string]string{"message": "hello"})
	callResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodToolsCall, protocol.CallToolParams{
		Name:      "mock.echo",
		Arguments: args,
	}))
	if callResp.Error != nil {
		t.Fatalf("tools/call error: %+v", callResp.Error)
	}
	var callResult protocol.CallToolResult
	if err := json.Unmarshal(callResp.Result, &callResult); err != nil {
		t.Fatal(err)
	}
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "hello" {
		t.Errorf("call result = %+v", callResult)
	}

	// DELETE /mcp terminates the session.
	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/mcp", nil)
	delReq.Header.Set("Mcp-Session-Id", sid)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE status = %d, want 204", delResp.StatusCode)
	}

	// follow-up POST with the same session id must fail (404 → JSON error).
	body, _ := json.Marshal(newReq(4, protocol.MethodToolsList, struct{}{}))
	httpReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Mcp-Session-Id", sid)
	httpReq.Header.Set("Content-Type", "application/json")
	res2, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusNotFound {
		t.Errorf("post-DELETE status = %d, want 404", res2.StatusCode)
	}
}

func TestE2E_UnknownToolNamespace(t *testing.T) {
	bin := e2eMockBinary(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{ID: "mock", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin}},
	})

	resp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersion: protocol.ProtocolVersion}))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}

	args, _ := json.Marshal(map[string]string{"x": "y"})
	out, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsCall, protocol.CallToolParams{
		Name:      "ghost.echo",
		Arguments: args,
	}))
	if out.Error == nil || out.Error.Code != protocol.ErrToolNotEnabled {
		t.Errorf("expected ErrToolNotEnabled, got %+v", out.Error)
	}

	out2, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodToolsCall, protocol.CallToolParams{
		Name:      "noprefix",
		Arguments: args,
	}))
	if out2.Error == nil || out2.Error.Code != protocol.ErrToolNotEnabled {
		t.Errorf("expected ErrToolNotEnabled for unqualified name, got %+v", out2.Error)
	}
}

func TestE2E_TwoServers_AggregatedToolsList(t *testing.T) {
	bin := e2eMockBinary(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{ID: "alpha", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin, Args: []string{"--name", "alpha"}}},
		{ID: "beta", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin, Args: []string{"--name", "beta"}}},
	})

	resp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersion: protocol.ProtocolVersion}))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}

	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsList, struct{}{}))
	if listResp.Error != nil {
		t.Fatalf("tools/list: %+v", listResp.Error)
	}
	var listResult protocol.ListToolsResult
	_ = json.Unmarshal(listResp.Result, &listResult)
	prefixes := map[string]int{}
	for _, tt := range listResult.Tools {
		parts := strings.SplitN(tt.Name, ".", 2)
		if len(parts) == 2 {
			prefixes[parts[0]]++
		}
	}
	if prefixes["alpha"] == 0 || prefixes["beta"] == 0 {
		t.Errorf("expected tools from alpha and beta, got %+v", prefixes)
	}
}

func TestE2E_GetSSE_ProgressForwarded(t *testing.T) {
	bin := e2eMockBinary(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{ID: "mock", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin}},
	})

	initResp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersion: protocol.ProtocolVersion}))
	if initResp.Error != nil {
		t.Fatal(initResp.Error)
	}

	// Subscribe to SSE before issuing the slow call.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/mcp", nil)
	sseReq.Header.Set("Mcp-Session-Id", sid)
	sseReq.Header.Set("Accept", "text/event-stream")
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", sseResp.StatusCode)
	}

	// Trigger slow tool with a progress token in a goroutine.
	go func() {
		args, _ := json.Marshal(map[string]int{"duration_ms": 200})
		params := protocol.CallToolParams{
			Name:      "mock.slow",
			Arguments: args,
			Meta:      json.RawMessage(`{"progressToken":"tk-1"}`),
		}
		_, _ = rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsCall, params))
	}()

	// Read SSE lines for up to 3 seconds, expecting at least one progress event.
	scanner := bufio.NewScanner(sseResp.Body)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	got := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && got == 0 {
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			var n protocol.Notification
			if err := json.Unmarshal([]byte(payload), &n); err == nil && n.Method == protocol.NotifProgress {
				got++
				break
			}
		}
	}
	if got == 0 {
		t.Errorf("did not observe a progress notification on SSE")
	}
}

func TestE2E_HTTPDownstream(t *testing.T) {
	// Spin up a tiny in-process JSON-RPC mock and point a server spec at it.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req protocol.Request
		_ = json.Unmarshal(body, &req)
		if req.IsNotification() {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		resp := protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: req.ID}
		switch req.Method {
		case protocol.MethodInitialize:
			b, _ := json.Marshal(protocol.InitializeResult{
				ProtocolVersion: protocol.ProtocolVersion,
				Capabilities:    protocol.ServerCapabilities{Tools: &protocol.ToolsCapability{}},
				ServerInfo:      protocol.Implementation{Name: "httpmock", Version: "0"},
			})
			resp.Result = b
		case protocol.MethodToolsList:
			b, _ := json.Marshal(protocol.ListToolsResult{Tools: []protocol.Tool{
				{Name: "ping", InputSchema: json.RawMessage(`{"type":"object"}`)},
			}})
			resp.Result = b
		case protocol.MethodToolsCall:
			b, _ := json.Marshal(protocol.CallToolResult{
				Content: []protocol.ContentBlock{{Type: "text", Text: "pong"}},
			})
			resp.Result = b
		default:
			resp.Error = protocol.NewError(protocol.ErrMethodNotFound, "unknown", nil)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mock.Close()

	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{ID: "h", Transport: "http", HTTP: &config.HTTPSpec{URL: mock.URL, Timeout: 2 * time.Second}},
	})

	resp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersion: protocol.ProtocolVersion}))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}

	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsList, struct{}{}))
	var listResult protocol.ListToolsResult
	_ = json.Unmarshal(listResp.Result, &listResult)
	if len(listResult.Tools) != 1 || listResult.Tools[0].Name != "h.ping" {
		t.Errorf("tools = %+v", listResult.Tools)
	}

	args, _ := json.Marshal(map[string]string{})
	callResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodToolsCall, protocol.CallToolParams{
		Name:      "h.ping",
		Arguments: args,
	}))
	if callResp.Error != nil {
		t.Fatalf("call err: %+v", callResp.Error)
	}
	var callResult protocol.CallToolResult
	_ = json.Unmarshal(callResp.Result, &callResult)
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "pong" {
		t.Errorf("call result = %+v", callResult)
	}
}

// TestE2E_RequiresInitializeBeforeOtherMethods verifies that a POST without a
// Mcp-Session-Id and method != initialize is rejected with 404. Previously
// the resolveSession helper would create a fresh session for any method,
// allowing clients to skip the MCP handshake.
func TestE2E_RequiresInitializeBeforeOtherMethods(t *testing.T) {
	srv, _ := startMcpDevServer(t, nil)

	body, _ := json.Marshal(newReq(1, protocol.MethodToolsList, struct{}{}))
	httpReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 (session required); got %d", res.StatusCode)
	}
}
