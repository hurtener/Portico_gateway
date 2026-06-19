// Package protocol defines the on-the-wire A2A (Agent-to-Agent) types
// Portico produces and consumes. Hand-rolled (no SDK dependency) so the
// project owns the wire format. Single source of truth — no other package
// defines A2A messages (AGENTS.md §13).
//
// This file carries the JSON-RPC 2.0 envelope only. A2A is itself a
// JSON-RPC 2.0 protocol. Task, Message, Part, Artifact, TaskStatus and the
// rest of the A2A task/message half live in separate later units — do
// NOT add them here.
package protocol

import "encoding/json"

// JSONRPCVersion is always "2.0". A2A is a JSON-RPC 2.0 protocol.
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request envelope as used by A2A.
//
// ID is RawMessage so we preserve the wire form (number, string, or null)
// for echo on the response side without forcing a type.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the request lacks an ID (i.e. it is a
// notification per JSON-RPC 2.0).
func (r Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// Response is a JSON-RPC 2.0 response. Result and Error are mutually
// exclusive.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no id).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Error is the JSON-RPC error structure carried by failed A2A responses.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error renders the error's Message for the error interface. Nil-safe so
// callers can compare against nil and still log without panicking.
func (e *Error) Error() string {
	if e == nil {
		return "<nil error>"
	}
	return e.Message
}
