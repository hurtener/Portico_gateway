package protocol

import "encoding/json"

// MCP method names for the server-initiated request channel introduced
// in this revision. Phase 5 wires elicitation/create; sampling/createMessage
// is reserved for a future phase.
const (
	MethodElicitationCreate = "elicitation/create"
)

// ElicitationCreateParams is the body of a server → client elicitation
// request. The schema mirrors the MCP spec verbatim.
type ElicitationCreateParams struct {
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema"`
	Meta            json.RawMessage `json:"_meta,omitempty"`
}

// ElicitationCreateResult is the client → server reply. Action is one of
// "accept" / "reject" / "cancel" per spec. Content carries the field
// values when Action == "accept".
type ElicitationCreateResult struct {
	Action  string          `json:"action"`
	Content json.RawMessage `json:"content,omitempty"`
}

// Elicitation actions.
const (
	ElicitActionAccept = "accept"
	ElicitActionReject = "reject"
	ElicitActionCancel = "cancel"
)
