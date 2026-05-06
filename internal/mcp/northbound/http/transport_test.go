package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// stubDispatcher satisfies the Dispatcher interface and does the bare
// minimum to let initialize succeed.
type stubDispatcher struct{}

func (stubDispatcher) HandleRequest(_ context.Context, _ *mcpgw.Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if req.Method == protocol.MethodInitialize {
		body, _ := json.Marshal(protocol.InitializeResult{
			ProtocolVersion: protocol.ProtocolVersion,
			ServerInfo:      protocol.Implementation{Name: "stub"},
		})
		return body, nil
	}
	return json.RawMessage(`{}`), nil
}

func (stubDispatcher) HandleNotification(_ context.Context, _ *mcpgw.Session, _ *protocol.Notification) {
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestOrigin_NoHeader_Allowed(t *testing.T) {
	h := NewHandlerWithConfig(mcpgw.NewSessionRegistry(), stubDispatcher{}, discardLogger(), HandlerConfig{})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	body := mustInit()
	req, _ := nethttp.NewRequest(nethttp.MethodPost, srv.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
}

func TestOrigin_Disallowed_Returns403(t *testing.T) {
	h := NewHandlerWithConfig(mcpgw.NewSessionRegistry(), stubDispatcher{}, discardLogger(), HandlerConfig{})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, _ := nethttp.NewRequest(nethttp.MethodPost, srv.URL, strings.NewReader(mustInit()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusForbidden {
		t.Errorf("status = %d; want 403", resp.StatusCode)
	}
}

func TestOrigin_Allowlist_Permits(t *testing.T) {
	h := NewHandlerWithConfig(mcpgw.NewSessionRegistry(), stubDispatcher{}, discardLogger(), HandlerConfig{
		AllowedOrigins: []string{"https://console.example.com"},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, _ := nethttp.NewRequest(nethttp.MethodPost, srv.URL, strings.NewReader(mustInit()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://console.example.com")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
}

func TestOrigin_DevModeAllowsLocalhost(t *testing.T) {
	h := NewHandlerWithConfig(mcpgw.NewSessionRegistry(), stubDispatcher{}, discardLogger(), HandlerConfig{
		AllowLocalhostOrigins: true,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:8080", "http://[::1]:9000"} {
		req, _ := nethttp.NewRequest(nethttp.MethodPost, srv.URL, strings.NewReader(mustInit()))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		resp, err := nethttp.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", origin, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != nethttp.StatusOK {
			t.Errorf("%s: status = %d; want 200", origin, resp.StatusCode)
		}
	}
}

func TestOrigin_Wildcard_AllowsAny(t *testing.T) {
	h := NewHandlerWithConfig(mcpgw.NewSessionRegistry(), stubDispatcher{}, discardLogger(), HandlerConfig{
		AllowedOrigins: []string{"*"},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, _ := nethttp.NewRequest(nethttp.MethodPost, srv.URL, strings.NewReader(mustInit()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://anywhere.example.com")
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
}

func TestSSE_RejectsNonSSEAccept(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	sess := sessions.Create("acme", "u")
	h := NewHandlerWithConfig(sessions, stubDispatcher{}, discardLogger(), HandlerConfig{AllowLocalhostOrigins: true})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, _ := nethttp.NewRequest(nethttp.MethodGet, srv.URL, nil)
	req.Header.Set("Mcp-Session-Id", sess.ID)
	req.Header.Set("Accept", "application/json") // intentionally not SSE
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusNotAcceptable {
		t.Errorf("status = %d; want 406", resp.StatusCode)
	}
}

func mustInit() string {
	body, _ := json.Marshal(protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`1`),
		Method:  protocol.MethodInitialize,
	})
	return string(body)
}
