package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2anb "github.com/hurtener/Portico_gateway/internal/a2a/northbound/http"
	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// fakeDispatcher records calls and returns canned results/errors.
type fakeDispatcher struct {
	lastTenant string
	lastPeer   string
	sendResult json.RawMessage
	task       *a2a.Task
	aerr       *a2a.Error
}

func (f *fakeDispatcher) SendMessage(_ context.Context, tenantID, peerID string, _ a2a.MessageSendParams) (json.RawMessage, *a2a.Error) {
	f.lastTenant, f.lastPeer = tenantID, peerID
	return f.sendResult, f.aerr
}
func (f *fakeDispatcher) GetTask(_ context.Context, tenantID, peerID string, _ a2a.TaskQueryParams) (*a2a.Task, *a2a.Error) {
	f.lastTenant, f.lastPeer = tenantID, peerID
	return f.task, f.aerr
}
func (f *fakeDispatcher) CancelTask(_ context.Context, tenantID, peerID string, _ a2a.TaskIDParams) (*a2a.Task, *a2a.Error) {
	f.lastTenant, f.lastPeer = tenantID, peerID
	return f.task, f.aerr
}

func cardProvider() a2anb.CardProvider {
	return func(_ context.Context, tenantID string) a2a.AgentCard {
		return a2a.AgentCard{Name: "Portico", URL: "/a2a", Version: "1.0.0", ProtocolVersion: a2a.SpecVersion}
	}
}

func withTenant(r *http.Request, tenantID string) *http.Request {
	return r.WithContext(tenant.With(r.Context(), tenant.Identity{TenantID: tenantID, Scopes: []string{"admin"}}))
}

func doRPC(t *testing.T, h *a2anb.Handler, tenantID, jsonBody string) a2a.Response {
	t.Helper()
	r := withTenant(httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(jsonBody)), tenantID)
	w := httptest.NewRecorder()
	h.RPC(w, r)
	var resp a2a.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

func TestAgentCard(t *testing.T) {
	h := a2anb.NewHandler(&fakeDispatcher{}, cardProvider(), nil)
	r := withTenant(httptest.NewRequest(http.MethodGet, "/a2a/.well-known/agent.json", nil), "t1")
	w := httptest.NewRecorder()
	h.AgentCard(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var card a2a.AgentCard
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if card.Name != "Portico" || card.ProtocolVersion != a2a.SpecVersion {
		t.Errorf("card = %+v", card)
	}
}

func TestAgentCard_NoTenant(t *testing.T) {
	h := a2anb.NewHandler(&fakeDispatcher{}, cardProvider(), nil)
	w := httptest.NewRecorder()
	h.AgentCard(w, httptest.NewRequest(http.MethodGet, "/a2a/.well-known/agent.json", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when no tenant", w.Code)
	}
}

func TestRPC_SendMessage_RoutesToPeer(t *testing.T) {
	disp := &fakeDispatcher{sendResult: json.RawMessage(`{"id":"task-1","kind":"task"}`)}
	h := a2anb.NewHandler(disp, cardProvider(), nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","messageId":"m1","parts":[]},"metadata":{"portico_peer":"a2a_123"}}}`
	resp := doRPC(t, h, "t1", body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if disp.lastTenant != "t1" || disp.lastPeer != "a2a_123" {
		t.Errorf("routed to tenant=%q peer=%q, want t1/a2a_123", disp.lastTenant, disp.lastPeer)
	}
	if string(resp.ID) != "1" {
		t.Errorf("id echo = %s", resp.ID)
	}
	if !strings.Contains(string(resp.Result), `"task-1"`) {
		t.Errorf("result = %s", resp.Result)
	}
}

func TestRPC_MissingPeerMetadata(t *testing.T) {
	h := a2anb.NewHandler(&fakeDispatcher{}, cardProvider(), nil)
	body := `{"jsonrpc":"2.0","id":2,"method":"message/send","params":{"message":{"role":"user","messageId":"m1","parts":[]}}}`
	resp := doRPC(t, h, "t1", body)
	if resp.Error == nil || resp.Error.Code != a2a.ErrInvalidParams {
		t.Fatalf("want ErrInvalidParams for missing peer metadata, got %+v", resp.Error)
	}
}

func TestRPC_DispatcherErrorPropagates(t *testing.T) {
	disp := &fakeDispatcher{aerr: a2a.NewError(a2a.ErrTaskNotFound, "no such task")}
	h := a2anb.NewHandler(disp, cardProvider(), nil)
	body := `{"jsonrpc":"2.0","id":3,"method":"tasks/get","params":{"id":"t","metadata":{"portico_peer":"a2a_1"}}}`
	resp := doRPC(t, h, "t1", body)
	if resp.Error == nil || resp.Error.Code != a2a.ErrTaskNotFound {
		t.Fatalf("dispatcher error should propagate, got %+v", resp.Error)
	}
}

func TestRPC_TasksGet_RoutesAndReturnsTask(t *testing.T) {
	disp := &fakeDispatcher{task: &a2a.Task{ID: "task-9", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}}
	h := a2anb.NewHandler(disp, cardProvider(), nil)
	body := `{"jsonrpc":"2.0","id":4,"method":"tasks/get","params":{"id":"task-9","metadata":{"portico_peer":"a2a_1"}}}`
	resp := doRPC(t, h, "t1", body)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.ID != "task-9" || task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("task = %+v", task)
	}
}

func TestRPC_UnknownMethod(t *testing.T) {
	h := a2anb.NewHandler(&fakeDispatcher{}, cardProvider(), nil)
	body := `{"jsonrpc":"2.0","id":5,"method":"message/stream","params":{}}`
	resp := doRPC(t, h, "t1", body)
	if resp.Error == nil || resp.Error.Code != a2a.ErrMethodNotFound {
		t.Fatalf("want ErrMethodNotFound, got %+v", resp.Error)
	}
}

func TestRPC_ParseError(t *testing.T) {
	h := a2anb.NewHandler(&fakeDispatcher{}, cardProvider(), nil)
	resp := doRPC(t, h, "t1", `{not json`)
	if resp.Error == nil || resp.Error.Code != a2a.ErrParseError {
		t.Fatalf("want ErrParseError, got %+v", resp.Error)
	}
}
