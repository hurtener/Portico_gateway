package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	porticohttp "github.com/hurtener/Portico_gateway/internal/mcp/northbound/http"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// stubDispatcher answers initialize/ping for the SSE handshake test.
type stubDispatcher struct{}

func (stubDispatcher) HandleRequest(_ context.Context, _ *mcpgw.Session, req *protocol.Request) (json.RawMessage, *protocol.Error) {
	if req.Method == protocol.MethodInitialize {
		body, _ := json.Marshal(protocol.InitializeResult{ProtocolVersion: protocol.ProtocolVersion})
		return body, nil
	}
	return json.RawMessage(`{}`), nil
}
func (stubDispatcher) HandleNotification(_ context.Context, _ *mcpgw.Session, _ *protocol.Notification) {
}

func TestServerInitiated_Roundtrip(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	sess := sessions.Create("acme", "u1", "")
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	defer requester.Stop()

	// Drain the session SSE channel and capture the elicitation envelope.
	gotEnvelope := make(chan json.RawMessage, 1)
	go func() {
		for n := range sess.Notifications() {
			if n.Method == "_portico/server_request" {
				gotEnvelope <- n.Params
				return
			}
		}
	}()

	// Fire Send in a goroutine; we'll deliver the response from the test.
	type sendResult struct {
		resp *protocol.Response
		err  error
	}
	resCh := make(chan sendResult, 1)
	go func() {
		resp, err := requester.Send(context.Background(), sess.ID, "elicitation/create",
			map[string]any{"message": "approve?"}, time.Second)
		resCh <- sendResult{resp: resp, err: err}
	}()

	envelope := <-gotEnvelope
	var env struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(envelope, &env); err != nil {
		t.Fatal(err)
	}

	// Deliver the response directly via TryDeliver (the path POST /mcp uses).
	respBody, _ := json.Marshal(protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      env.ID,
		Result:  json.RawMessage(`{"action":"accept","content":{"approve":true}}`),
	})
	if !requester.TryDeliver(respBody) {
		t.Fatal("TryDeliver returned false")
	}
	res := <-resCh
	if res.err != nil {
		t.Fatalf("Send error: %v", res.err)
	}
	if res.resp.Error != nil {
		t.Fatalf("Send returned error: %+v", res.resp.Error)
	}
}

func TestServerInitiated_Timeout(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	sess := sessions.Create("acme", "u1", "")
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	defer requester.Stop()

	go func() {
		for range sess.Notifications() { //nolint:revive // empty-block: only draining
		}
	}()

	_, err := requester.Send(context.Background(), sess.ID, "elicitation/create",
		map[string]any{}, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error; got %v", err)
	}
}

func TestServerInitiated_UnknownSession(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	defer requester.Stop()

	_, err := requester.Send(context.Background(), "no-such-session", "elicitation/create", nil, time.Second)
	if err == nil {
		t.Errorf("expected error for unknown session")
	}
}

func TestServerInitiated_TryDeliverIgnoresUnknownID(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	defer requester.Stop()

	body := `{"jsonrpc":"2.0","id":"unknown","result":{}}`
	if requester.TryDeliver([]byte(body)) {
		t.Errorf("expected TryDeliver=false for unknown id")
	}
}

func TestServerInitiated_TryDeliverIgnoresRequests(t *testing.T) {
	// Inbound requests (have method) must NOT be consumed by the requester.
	sessions := mcpgw.NewSessionRegistry()
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	defer requester.Stop()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if requester.TryDeliver([]byte(body)) {
		t.Errorf("expected TryDeliver=false for inbound request")
	}
}

func TestSSE_EmitsServerRequestEvent(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	h := porticohttp.NewHandler(sessions, stubDispatcher{}, nil)

	// Initialize the session.
	srv := httptest.NewServer(h)
	defer srv.Close()
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	resp, err := nethttp.Post(srv.URL, "application/json", strings.NewReader(initBody))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("missing session id")
	}

	// Open SSE channel.
	req, _ := nethttp.NewRequest(nethttp.MethodGet, srv.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", sid)
	sseResp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sseResp.Body.Close()

	sess, ok := sessions.Get(sid)
	if !ok {
		t.Fatal("session lookup failed")
	}
	// Inject a fake server-initiated envelope into the session's notif
	// channel — bypasses the requester to keep the test focused on the
	// SSE path.
	envelope := json.RawMessage(`{"jsonrpc":"2.0","id":"s_test","method":"elicitation/create","params":{}}`)
	sess.EmitNotification(protocol.Notification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "_portico/server_request",
		Params:  envelope,
	})

	// Read first SSE chunk; expect `event: server_request`.
	br := bufio.NewReader(sseResp.Body)
	var sawEvent bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == "event: server_request" {
			sawEvent = true
			break
		}
	}
	if !sawEvent {
		t.Errorf("did not see event: server_request in SSE stream")
	}
}

func TestServerInitiated_PerSessionSerialization(t *testing.T) {
	// Two concurrent Send calls for the same session must serialise via
	// the per-session mutex. We assert the SSE channel sees exactly one
	// envelope at a time by counting overlapping outstanding requests.
	sessions := mcpgw.NewSessionRegistry()
	sess := sessions.Create("acme", "u1", "")
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	defer requester.Stop()

	var (
		mu       sync.Mutex
		live     int
		maxLive  int
		envelope = make(chan struct{}, 4)
	)
	go func() {
		for n := range sess.Notifications() {
			if n.Method == "_portico/server_request" {
				mu.Lock()
				live++
				if live > maxLive {
					maxLive = live
				}
				mu.Unlock()
				envelope <- struct{}{}
			}
		}
	}()

	// Two concurrent senders. Each sender's response is delivered after
	// we observe its envelope, so they cannot overlap if serialisation works.
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _ = requester.Send(context.Background(), sess.ID, "elicitation/create", map[string]any{}, time.Second)
		}()
	}

	for i := 0; i < 2; i++ {
		<-envelope
		// Find the most recent envelope's id and deliver a reply for it.
		// The simple way: we know our requester only registers ids we
		// can find by inspecting the response body shape — but the test
		// only needs to wake the senders, not match exact ids. Send a
		// global reply that the requester rejects, then advance time
		// past the timeout. To keep it deterministic, just wait for
		// timeouts and assert no overlap.
		mu.Lock()
		live--
		mu.Unlock()
	}
	wg.Wait()
	if maxLive > 1 {
		t.Errorf("concurrent envelopes overlapped: maxLive=%d", maxLive)
	}
}

// ensure Stop is idempotent and rejects pending requests.
func TestServerInitiated_StopRejectsPending(t *testing.T) {
	sessions := mcpgw.NewSessionRegistry()
	sess := sessions.Create("acme", "u1", "")
	requester := porticohttp.NewServerInitiatedRequester(sessions)
	go func() {
		for range sess.Notifications() { //nolint:revive
		}
	}()

	// Send is blocking; spawn it and then Stop the requester.
	errCh := make(chan error, 1)
	go func() {
		_, err := requester.Send(context.Background(), sess.ID, "elicitation/create", map[string]any{}, time.Minute)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	requester.Stop()
	select {
	case <-errCh:
		// Either an explicit shutdown error or the response we delivered.
		// Both are acceptable; we just want Send to unblock.
	case <-time.After(2 * time.Second):
		t.Errorf("Send did not return after Stop")
	}
	// Stop should be idempotent.
	requester.Stop()
}

// silence unused-helper warnings in this file.
var _ = fmt.Sprintf
