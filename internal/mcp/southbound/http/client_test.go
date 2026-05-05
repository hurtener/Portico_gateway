package http_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	httpcli "github.com/hurtener/Portico_gateway/internal/mcp/southbound/http"
)

// jsonrpcMock is a tiny stateful JSON-RPC server good enough to drive the
// southbound HTTP client through initialize -> tools/list -> tools/call.
type jsonrpcMock struct {
	flake bool // when true, first request returns 503 then 200 thereafter
	calls int
}

func (m *jsonrpcMock) ServeHTTP(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method == nethttp.MethodDelete {
		w.WriteHeader(nethttp.StatusNoContent)
		return
	}
	m.calls++
	if m.flake && m.calls == 1 {
		w.WriteHeader(nethttp.StatusServiceUnavailable)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req protocol.Request
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(nethttp.StatusBadRequest)
		return
	}
	resp := protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: req.ID}
	switch req.Method {
	case protocol.MethodInitialize:
		w.Header().Set("Mcp-Session-Id", "sid-1")
		body, _ := json.Marshal(protocol.InitializeResult{
			ProtocolVersion: protocol.ProtocolVersion,
			Capabilities:    protocol.ServerCapabilities{Tools: &protocol.ToolsCapability{}},
			ServerInfo:      protocol.Implementation{Name: "httpmock", Version: "0.0.1"},
		})
		resp.Result = body
	case protocol.NotifInitialized:
		w.WriteHeader(nethttp.StatusAccepted)
		return
	case protocol.MethodPing:
		resp.Result = json.RawMessage(`{}`)
	case protocol.MethodToolsList:
		body, _ := json.Marshal(protocol.ListToolsResult{Tools: []protocol.Tool{
			{Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`)},
		}})
		resp.Result = body
	case protocol.MethodToolsCall:
		body, _ := json.Marshal(protocol.CallToolResult{
			Content: []protocol.ContentBlock{{Type: "text", Text: "ok"}},
		})
		resp.Result = body
	default:
		resp.Error = protocol.NewError(protocol.ErrMethodNotFound, "unknown", nil)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func newHTTPClient(t *testing.T, srv *httptest.Server) *httpcli.Client {
	t.Helper()
	c := httpcli.New(httpcli.Config{
		ServerID: "httpmock",
		URL:      srv.URL,
		Timeout:  3 * time.Second,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestHTTPClient_InitializeAndListTools(t *testing.T) {
	srv := httptest.NewServer(&jsonrpcMock{})
	defer srv.Close()
	c := newHTTPClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if c.ServerInfo().Name != "httpmock" {
		t.Errorf("server name = %q", c.ServerInfo().Name)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Errorf("tools = %+v", tools)
	}
	if err := c.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
	res, err := c.CallTool(ctx, "echo", json.RawMessage(`{}`), nil, nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.Content[0].Text != "ok" {
		t.Errorf("call result = %+v", res)
	}
}

func TestHTTPClient_SessionIDCarried(t *testing.T) {
	var lastSID string
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		// First request: initialize, returns Mcp-Session-Id.
		// Subsequent requests: must carry it back.
		body, _ := io.ReadAll(r.Body)
		var req protocol.Request
		_ = json.Unmarshal(body, &req)
		if req.Method == protocol.MethodInitialize {
			w.Header().Set("Mcp-Session-Id", "sid-XYZ")
			res := protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: req.ID}
			body, _ := json.Marshal(protocol.InitializeResult{
				ProtocolVersion: protocol.ProtocolVersion,
				Capabilities:    protocol.ServerCapabilities{},
				ServerInfo:      protocol.Implementation{Name: "h", Version: "0"},
			})
			res.Result = body
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(res)
			return
		}
		lastSID = r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Content-Type", "application/json")
		body2, _ := json.Marshal(protocol.ListToolsResult{Tools: []protocol.Tool{}})
		_ = json.NewEncoder(w).Encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: req.ID, Result: body2})
	}))
	defer srv.Close()

	c := newHTTPClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	if lastSID != "sid-XYZ" {
		t.Errorf("subsequent request didn't carry session id; got %q", lastSID)
	}
}

func TestHTTPClient_TransportError(t *testing.T) {
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := newHTTPClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Start(ctx)
	if err == nil {
		t.Fatal("expected init failure on 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v", err)
	}
}
