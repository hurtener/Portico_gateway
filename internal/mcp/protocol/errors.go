package protocol

import "encoding/json"

// JSON-RPC error codes plus Portico-defined codes in the implementation
// reserved range (-32099..-32000). New codes go here and only here.
const (
	// JSON-RPC standard
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603

	// JSON-RPC: client cancelled (per spec). We mostly use this on the
	// outbound path when forwarding cancellations; the response form is rare.
	ErrCancelled = -32800

	// Portico-defined (-32000..-32099)
	ErrApprovalRequired    = -32001 // Phase 5
	ErrUpstreamUnavailable = -32002 // downstream transport error
	ErrPolicyDenied        = -32003 // Phase 5
	ErrToolNotEnabled      = -32004 // tool not visible / wrong namespace
	ErrTenantInactive      = -32005 // future
)

// NewError builds an *Error with optional structured data.
func NewError(code int, msg string, data any) *Error {
	e := &Error{Code: code, Message: msg}
	if data != nil {
		raw, err := json.Marshal(data)
		if err == nil {
			e.Data = raw
		}
	}
	return e
}
