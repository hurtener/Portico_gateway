// Tests for the a2amock handler. The handler is exercised via
// httptest.NewServer so each test gets an isolated listener and a
// well-known base URL.
package a2amock_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/examples/servers/mock/a2amock"
	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
)

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(a2amock.Handler(a2amock.Options{}))
}

func TestMockA2A_AgentCard(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET agent.json: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if card.Name != "mocka2a" {
		t.Errorf("Name = %q, want %q", card.Name, "mocka2a")
	}
	if card.URL != "/a2a" {
		t.Errorf("URL = %q, want /a2a", card.URL)
	}
	if card.ProtocolVersion != a2a.SpecVersion {
		t.Errorf("ProtocolVersion = %q, want %q", card.ProtocolVersion, a2a.SpecVersion)
	}
	if len(card.Skills) != 2 {
		t.Fatalf("len(Skills) = %d, want 2", len(card.Skills))
	}
	foundEcho := false
	for _, s := range card.Skills {
		if s.ID == "echo" {
			foundEcho = true
		}
	}
	if !foundEcho {
		t.Errorf("Skills missing id=echo, got %+v", card.Skills)
	}
}

func TestMockA2A_MessageSend_Echo(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	params := a2a.MessageSendParams{
		Message: a2a.Message{
			Role: a2a.RoleUser,
			Parts: []a2a.Part{
				{Kind: a2a.PartKindText, Text: "ping"},
			},
		},
	}
	paramsJSON, _ := json.Marshal(params)

	req := a2a.Request{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      json.RawMessage(`"req-1"`),
		Method:  a2a.MethodMessageSend,
		Params:  paramsJSON,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s, want 200", resp.StatusCode, string(raw))
	}

	var rpcResp a2a.Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("Error = %+v, want nil", rpcResp.Error)
	}

	var task a2a.Task
	if err := json.Unmarshal(rpcResp.Result, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("task.Status.State = %q, want %q", task.Status.State, a2a.TaskStateCompleted)
	}
	if len(task.Artifacts) != 1 {
		t.Fatalf("len(task.Artifacts) = %d, want 1", len(task.Artifacts))
	}
	art := task.Artifacts[0]
	if len(art.Parts) == 0 || art.Parts[0].Kind != a2a.PartKindText {
		t.Fatalf("artifact has no text part: %+v", art.Parts)
	}
	if got := art.Parts[0].Text; got != "ping" {
		t.Errorf("artifact echo text = %q, want %q", got, "ping")
	}
}

func TestMockA2A_TasksCancel(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	params := a2a.TaskIDParams{ID: "t-9"}
	paramsJSON, _ := json.Marshal(params)

	req := a2a.Request{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      json.RawMessage(`"req-2"`),
		Method:  a2a.MethodTasksCancel,
		Params:  paramsJSON,
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s, want 200", resp.StatusCode, string(raw))
	}

	var rpcResp a2a.Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("Error = %+v, want nil", rpcResp.Error)
	}

	var task a2a.Task
	if err := json.Unmarshal(rpcResp.Result, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.ID != "t-9" {
		t.Errorf("task.ID = %q, want %q", task.ID, "t-9")
	}
	if task.Status.State != a2a.TaskStateCanceled {
		t.Errorf("task.Status.State = %q, want %q", task.Status.State, a2a.TaskStateCanceled)
	}
}

func TestMockA2A_MethodNotFound(t *testing.T) {
	srv := newServer(t)
	defer srv.Close()

	req := a2a.Request{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      json.RawMessage(`"req-3"`),
		Method:  "does/not/exist",
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s, want 200 (JSON-RPC error returned in body)", resp.StatusCode, string(raw))
	}

	var rpcResp a2a.Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatalf("Error = nil, want non-nil")
	}
	if rpcResp.Error.Code != a2a.ErrMethodNotFound {
		t.Errorf("Error.Code = %d, want %d", rpcResp.Error.Code, a2a.ErrMethodNotFound)
	}
}
