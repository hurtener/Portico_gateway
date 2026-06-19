// Package a2amock provides an in-process and standalone-binary A2A peer for
// testing Portico's A2A southbound. The implementation is deliberately
// minimal: discovery via the agent card, plus the unary half of the
// JSON-RPC 2.0 surface (message/send, tasks/get, tasks/cancel). Streaming
// (message/stream, tasks/resubscribe, push-notification-config) is added
// in a separate later unit.
//
// The package is the A2A analog of examples/servers/mock, which provides
// the MCP mock core. A2A is JSON-RPC 2.0 over HTTP — unlike MCP which
// uses stdio line-framing — so the handler is an http.Handler rather than
// a Run(ctx, in, out) loop.
package a2amock

import (
	"encoding/json"
	"net/http"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
)

// Options configures the mock A2A peer.
type Options struct {
	// Name is the agent-card name advertised by this peer. Defaults to
	// "mocka2a" when empty.
	Name string
	// CardPath is the URL path the agent card is served at. Defaults to
	// "/.well-known/agent.json" when empty.
	CardPath string
}

// AgentCard returns a deterministic agent card for the given name. The
// card is fixed across calls for the same name so integration tests can
// assert exact values.
func AgentCard(name string) a2a.AgentCard {
	if name == "" {
		name = "mocka2a"
	}
	return a2a.AgentCard{
		Name:            name,
		URL:             "/a2a",
		Version:         "0.1.0",
		ProtocolVersion: a2a.SpecVersion,
		Capabilities:    a2a.AgentCapabilities{Streaming: false},
		Skills: []a2a.AgentSkill{
			{ID: "echo", Name: "Echo", Description: "Echoes the input back"},
			{ID: "slow", Name: "Slow", Description: "A long-running task (streaming added later)"},
		},
	}
}

// Handler returns an http.Handler that serves the agent card plus the
// unary task surface (message/send, tasks/get, tasks/cancel).
//
// The handler is deterministic: no time.Now, no randomness. Tests can
// assert the exact JSON-RPC responses produced.
func Handler(opts Options) http.Handler {
	if opts.Name == "" {
		opts.Name = "mocka2a"
	}
	if opts.CardPath == "" {
		opts.CardPath = "/.well-known/agent.json"
	}
	mux := http.NewServeMux()
	mux.HandleFunc(opts.CardPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AgentCard(opts.Name))
	})
	mux.HandleFunc("/a2a", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleA2A(w, r)
	})
	return mux
}

// handleA2A dispatches a JSON-RPC 2.0 request to the unary task surface.
func handleA2A(w http.ResponseWriter, r *http.Request) {
	var req a2a.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, a2a.ErrParseError, "parse error")
		return
	}
	switch req.Method {
	case a2a.MethodMessageSend:
		var p a2a.MessageSendParams
		_ = json.Unmarshal(req.Params, &p)
		writeMessageSendResult(w, req.ID, p)
	case a2a.MethodTasksGet:
		var p a2a.TaskQueryParams
		_ = json.Unmarshal(req.Params, &p)
		writeTaskResult(w, req.ID, a2a.Task{
			ID:   p.ID,
			Kind: a2a.KindTask,
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
			},
		})
	case a2a.MethodTasksCancel:
		var p a2a.TaskIDParams
		_ = json.Unmarshal(req.Params, &p)
		writeTaskResult(w, req.ID, a2a.Task{
			ID:   p.ID,
			Kind: a2a.KindTask,
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCanceled,
			},
		})
	default:
		writeError(w, req.ID, a2a.ErrMethodNotFound, "method not found")
	}
}

// writeMessageSendResult emits a task that echoes the inbound text part.
func writeMessageSendResult(w http.ResponseWriter, id json.RawMessage, p a2a.MessageSendParams) {
	text := firstText(p.Message.Parts)
	task := a2a.Task{
		ID:        "task-mock-1",
		ContextID: "ctx-mock-1",
		Kind:      a2a.KindTask,
		Status:    a2a.TaskStatus{State: a2a.TaskStateCompleted},
		Artifacts: []a2a.Artifact{
			{
				ArtifactID: "art-1",
				Name:       "echo",
				Parts: []a2a.Part{
					{Kind: a2a.PartKindText, Text: text},
				},
			},
		},
	}
	writeTaskResult(w, id, task)
}

// writeTaskResult writes a 200 OK JSON-RPC response whose result is the
// task.
func writeTaskResult(w http.ResponseWriter, id json.RawMessage, task a2a.Task) {
	body, err := json.Marshal(task)
	if err != nil {
		writeError(w, id, a2a.ErrInternalError, "marshal error")
		return
	}
	writeResult(w, id, body)
}

// writeResult marshals an a2a.Response whose result is the supplied body
// and writes it as application/json.
func writeResult(w http.ResponseWriter, id json.RawMessage, result json.RawMessage) {
	resp := a2a.Response{JSONRPC: a2a.JSONRPCVersion, ID: id, Result: result}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeError marshals an a2a.Response whose error has the supplied code
// and message, and writes it as application/json.
func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	resp := a2a.Response{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      id,
		Error:   a2a.NewError(code, msg),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// firstText returns the text of the first text Part in parts, or "" if
// none is present.
func firstText(parts []a2a.Part) string {
	for _, p := range parts {
		if p.Kind == a2a.PartKindText {
			return p.Text
		}
	}
	return ""
}
