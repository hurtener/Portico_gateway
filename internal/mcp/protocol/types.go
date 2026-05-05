// Package protocol defines the on-the-wire MCP types Portico produces and
// consumes. These are intentionally hand-rolled (no SDK dependency) so that
// the project owns the wire format end-to-end. If a Go SDK becomes
// authoritative later, swap the types behind this same package boundary.
package protocol

import "encoding/json"

// ProtocolVersion is the MCP protocol revision Portico targets.
// Bumping the version is an RFC change — see AGENTS.md §8.
const ProtocolVersion = "2025-06-18"

// JSONRPCVersion is always "2.0".
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request envelope.
//
// ID is RawMessage so we preserve the wire form (number, string, or null) for
// echo on the response side without forcing a type.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the request lacks an ID (i.e. it's a
// notification per JSON-RPC 2.0).
func (r Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// Response is a JSON-RPC 2.0 response. Result and Error are mutually exclusive.
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

// Error is the JSON-RPC error structure.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil error>"
	}
	return e.Message
}

// ----- Initialize ---------------------------------------------------------

type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation     `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ----- Capabilities -------------------------------------------------------

type ClientCapabilities struct {
	Roots        *RootsCapability           `json:"roots,omitempty"`
	Sampling     *SamplingCapability        `json:"sampling,omitempty"`
	Elicitation  *ElicitCapability          `json:"elicitation,omitempty"`
	Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

type ServerCapabilities struct {
	Tools        *ToolsCapability           `json:"tools,omitempty"`
	Resources    *ResourcesCapability       `json:"resources,omitempty"`
	Prompts      *PromptsCapability         `json:"prompts,omitempty"`
	Logging      *LoggingCapability         `json:"logging,omitempty"`
	Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type LoggingCapability struct{}

type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type SamplingCapability struct{}

type ElicitCapability struct{}

// ----- Tools --------------------------------------------------------------

type Tool struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Meta carries _meta from the spec; progressToken lives here.
	Meta json.RawMessage `json:"_meta,omitempty"`
}

type CallToolResult struct {
	Content           []ContentBlock  `json:"content"`
	IsError           bool            `json:"isError,omitempty"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

type ContentBlock struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	Data     string       `json:"data,omitempty"`
	MimeType string       `json:"mimeType,omitempty"`
	Resource *ResourceRef `json:"resource,omitempty"`
}

type ResourceRef struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// ----- Cancellation / progress -------------------------------------------

type CancelledParams struct {
	RequestID json.RawMessage `json:"requestId"`
	Reason    string          `json:"reason,omitempty"`
}

type ProgressParams struct {
	ProgressToken json.RawMessage `json:"progressToken"`
	Progress      float64         `json:"progress"`
	Total         *float64        `json:"total,omitempty"`
	Message       string          `json:"message,omitempty"`
}
