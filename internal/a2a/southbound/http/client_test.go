package http_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	httpcli "github.com/hurtener/Portico_gateway/internal/a2a/southbound/http"
)

// newSlog returns a silent logger for tests.
func newSlog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writeJSONRPC writes a JSON-RPC 2.0 Response with the supplied result
// or error and echoes the request id.
func writeJSONRPC(w http.ResponseWriter, id json.RawMessage, result json.RawMessage, err *a2a.Error) {
	w.Header().Set("Content-Type", "application/json")
	resp := a2a.Response{JSONRPC: a2a.JSONRPCVersion, ID: id, Result: result, Error: err}
	_ = json.NewEncoder(w).Encode(resp)
}

func newClient(t *testing.T, url string, opts ...func(*httpcli.Config)) *httpcli.Client {
	t.Helper()
	cfg := httpcli.Config{
		PeerID:   "mock-peer",
		Endpoint: url,
		Timeout:  3 * time.Second,
		Logger:   newSlog(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	c := httpcli.New(cfg)
	t.Cleanup(func() { _ = c.Close(t.Context()) })
	return c
}

func TestA2AClient_SendMessage_Unary(t *testing.T) {
	taskResult, err := json.Marshal(a2a.Task{
		ID:     "task-1",
		Kind:   a2a.KindTask,
		Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req a2a.Request
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.JSONRPC != a2a.JSONRPCVersion {
			t.Errorf("jsonrpc=%q want %q", req.JSONRPC, a2a.JSONRPCVersion)
		}
		if len(req.ID) == 0 {
			t.Errorf("missing id")
		}
		if req.Method != a2a.MethodMessageSend {
			t.Errorf("method=%q want %q", req.Method, a2a.MethodMessageSend)
		}
		writeJSONRPC(w, req.ID, taskResult, nil)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	raw, err := c.SendMessage(t.Context(), a2a.MessageSendParams{
		Message: a2a.Message{Role: a2a.RoleUser, MessageID: "m-1"},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	var out a2a.Task
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode raw result: %v", err)
	}
	if out.ID != "task-1" || out.Status.State != a2a.TaskStateSubmitted {
		t.Errorf("decoded task = %+v", out)
	}
}

func TestA2AClient_GetTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req a2a.Request
		_ = json.Unmarshal(body, &req)
		if req.Method != a2a.MethodTasksGet {
			t.Errorf("method=%q want %q", req.Method, a2a.MethodTasksGet)
		}
		body, _ = json.Marshal(a2a.Task{
			ID:     "task-7",
			Status: a2a.TaskStatus{State: a2a.TaskStateWorking},
		})
		writeJSONRPC(w, req.ID, body, nil)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	task, err := c.GetTask(t.Context(), a2a.TaskQueryParams{ID: "task-7"})
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.ID != "task-7" || task.Status.State != a2a.TaskStateWorking {
		t.Errorf("task=%+v", task)
	}
}

func TestA2AClient_FetchAgentCard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method=%q want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a2a.AgentCard{
			Name:            "mock-peer",
			URL:             "http://peers.example/a2a",
			Version:         "0.0.1",
			ProtocolVersion: a2a.SpecVersion,
			Skills: []a2a.AgentSkill{
				{ID: "greet", Name: "Greeter", Description: "Says hello"},
			},
		})
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	card, err := c.FetchAgentCard(t.Context(), srv.URL+"/.well-known/agent.json")
	if err != nil {
		t.Fatalf("FetchAgentCard: %v", err)
	}
	if card.Name != "mock-peer" || card.URL != "http://peers.example/a2a" {
		t.Errorf("card=%+v", card)
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "greet" {
		t.Errorf("skills=%+v", card.Skills)
	}
}

func TestA2AClient_ProtocolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req a2a.Request
		_ = json.Unmarshal(body, &req)
		writeJSONRPC(w, req.ID, nil, &a2a.Error{Code: a2a.ErrTaskNotFound, Message: "no such task"})
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	_, err := c.GetTask(t.Context(), a2a.TaskQueryParams{ID: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
	pe := httpcli.AsProtocolError(err)
	if pe == nil || pe.Code != a2a.ErrTaskNotFound {
		t.Errorf("protocol error = %+v, want code %d", pe, a2a.ErrTaskNotFound)
	}
}

func TestA2AClient_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	_, err := c.GetTask(t.Context(), a2a.TaskQueryParams{ID: "x"})
	if err == nil {
		t.Fatal("expected error on 503")
	}
	pe := httpcli.AsProtocolError(err)
	if pe == nil || pe.Code != a2a.ErrInternalError {
		t.Errorf("protocol error = %+v, want code %d", pe, a2a.ErrInternalError)
	}
}

func TestA2AClient_AuthHeaderSet(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req a2a.Request
		_ = json.Unmarshal(body, &req)
		writeJSONRPC(w, req.ID, json.RawMessage(`{}`), nil)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, func(cfg *httpcli.Config) {
		cfg.AuthHeader = "Bearer test-token-12345"
	})
	if _, err := c.SendMessage(t.Context(), a2a.MessageSendParams{
		Message: a2a.Message{Role: a2a.RoleUser, MessageID: "m-1"},
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if seenAuth != "Bearer test-token-12345" {
		t.Errorf("auth header = %q, want %q", seenAuth, "Bearer test-token-12345")
	}
}
