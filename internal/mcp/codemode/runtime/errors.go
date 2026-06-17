package runtime

import "fmt"

// Sandbox error codes. These are stable strings surfaced to clients (via the
// meta-tool handler, which maps them onto the JSON-RPC error envelope) and
// recorded in audit. They name the precise failure so an operator can tell a
// budget trip from a safety rejection from a tool error at a glance.
const (
	// CodeUnsafeCall — the static safety gate rejected the snippet before
	// execution: a load statement, a disallowed built-in, or a reference to a
	// name that was never bound. Carries the offending identifier in Detail.
	CodeUnsafeCall = "code_mode.unsafe_call"

	// CodeBudgetExceeded — an execution budget tripped. Detail names which one:
	// "steps", "wall_clock", "tool_calls", or "output_bytes".
	CodeBudgetExceeded = "code_mode.budget_exceeded"

	// CodeCompileError — the snippet failed to parse or compile (syntax error).
	CodeCompileError = "code_mode.compile_error"

	// CodeRuntimeError — the snippet raised a Starlark runtime error that was
	// not a budget trip or a tool error (e.g. a type error, a fail() call).
	CodeRuntimeError = "code_mode.runtime_error"

	// CodeToolError — a tool call issued from inside the sandbox failed at the
	// governance layer or downstream (policy denial, upstream error). The
	// governance decision is unchanged from a direct tools/call; the sandbox
	// merely surfaces it.
	CodeToolError = "code_mode.tool_error"

	// CodeApprovalRequired — a tool call inside the sandbox needs operator
	// approval; the execution suspended and a continuation token was issued.
	CodeApprovalRequired = "code_mode.approval_required"
)

// Budget dimension names used in SandboxError.Detail when Code is
// CodeBudgetExceeded.
const (
	BudgetSteps       = "steps"
	BudgetWallClock   = "wall_clock"
	BudgetToolCalls   = "tool_calls"
	BudgetOutputBytes = "output_bytes"
)

// SandboxError is the typed failure returned by the runtime. Code is one of the
// stable code_mode.* strings above; Detail carries the specific offender (an
// identifier, a budget dimension, a tool name); Cause is the wrapped underlying
// error when one exists.
type SandboxError struct {
	Code   string
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *SandboxError) Error() string {
	switch {
	case e.Detail != "" && e.Cause != nil:
		return fmt.Sprintf("%s (%s): %v", e.Code, e.Detail, e.Cause)
	case e.Detail != "":
		return fmt.Sprintf("%s (%s)", e.Code, e.Detail)
	case e.Cause != nil:
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	default:
		return e.Code
	}
}

// Unwrap exposes the wrapped cause for errors.Is / errors.As.
func (e *SandboxError) Unwrap() error { return e.Cause }

// newUnsafe builds an unsafe-call error naming the offending identifier.
func newUnsafe(identifier string) *SandboxError {
	return &SandboxError{Code: CodeUnsafeCall, Detail: identifier}
}

// newBudget builds a budget-exceeded error naming the tripped dimension.
func newBudget(dimension string) *SandboxError {
	return &SandboxError{Code: CodeBudgetExceeded, Detail: dimension}
}
