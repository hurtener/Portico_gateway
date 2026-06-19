package protocol

// Error codes — JSON-RPC standard plus A2A-specific codes in the
// implementation reserved range. New codes go here and only here.
const (
	// JSON-RPC standard.
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603

	// A2A-specific (-32001..-32006 per the A2A spec).
	ErrTaskNotFound                 = -32001 // referenced task_id does not exist
	ErrTaskNotCancelable            = -32002 // task is already in a terminal state
	ErrPushNotificationNotSupported = -32003 // peer does not implement push notifications
	ErrUnsupportedOperation         = -32004 // requested operation is not supported by this peer
	ErrContentTypeNotSupported      = -32005 // Part.contentType is not acceptable to this peer
	ErrInvalidAgentResponse         = -32006 // agent returned a response that could not be parsed/validated
)

// NewError builds an *Error with the given code and message. Data is left
// nil; callers that need structured data should set Error.Data after
// construction.
func NewError(code int, msg string) *Error {
	return &Error{Code: code, Message: msg}
}
