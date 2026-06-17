package runtime

import (
	"context"
	"encoding/json"
	"time"
)

// This file implements the runtime half of the approval suspend/resume cycle
// (threat-model class C4). When an in-sandbox tool call returns
// approval_required, the execution cannot proceed — Starlark frames are not
// serialisable — so instead of freezing the interpreter we capture everything a
// DETERMINISTIC REPLAY needs and abort. The handler persists that as a
// continuation; on resume it re-runs the identical snippet, serving the cached
// results for the calls that already completed and re-dispatching the awaited
// call (now approved) live. Determinism rests on three invariants the runtime
// guarantees: the snapshot is immutable for the execution's lifetime, the clock
// is frozen per execution (so time.now() returns the same value on replay), and
// every exposed binding is pure given identical inputs.

// ResumeState drives a replay. CachedResults holds the JSON result of each tool
// call that completed before the suspend, indexed by call ordinal: call i for
// i < len(CachedResults) is served from the cache WITHOUT re-dispatching (so a
// prior write is never re-executed). The call at index len(CachedResults) is
// the awaited one; it re-dispatches live with ApprovalID threaded onto its
// context so the governed approval gate recognises the prior grant rather than
// prompting again. A nil ResumeState means a fresh execution.
type ResumeState struct {
	CachedResults []json.RawMessage
	ApprovalID    string
}

// Suspension is the typed signal an Execute call returns when the snippet hit an
// approval-required tool call. It is an error (so it travels the normal return
// path) but carries the full continuation payload the handler persists. Steps
// and ToolCalls are reported for telemetry parity with Result.
type Suspension struct {
	// CallIndex is the ordinal of the tool call awaiting approval. It equals
	// len(CachedResults): the awaited call is the first one NOT in the cache.
	CallIndex int
	// ApprovalID is the approval the governed dispatcher opened for the awaited
	// call. The resume threads it back so the gate honours the grant.
	ApprovalID string
	// Tool is the namespaced tool name awaiting approval (for the response/audit).
	Tool string
	// CachedResults are the JSON results of calls 0..CallIndex-1, in order. The
	// resume replays exactly these before re-dispatching the awaited call.
	CachedResults []json.RawMessage
	// PrintBuffer is the redacted print() output accumulated up to the suspend.
	// Persisted for audit only — replay regenerates output by re-execution.
	PrintBuffer string
	// Clock is the frozen execution timestamp. The resume MUST reuse it so
	// time.now() replays identically (determinism, class C4).
	Clock time.Time
	// Steps and ToolCalls are the counts consumed up to the suspend.
	Steps     uint64
	ToolCalls int
}

// Error implements the error interface. The handler matches with errors.As and
// never surfaces this string to clients verbatim.
func (s *Suspension) Error() string { return "code_mode.approval_required (execution suspended)" }

// resumeApprovalIDKey is the context key under which the runtime stashes the
// granted approval id for the awaited tool call. The mcpgw policy pipeline reads
// it (via ResumeApprovalIDFrom) and sets approval.CallContext.ApprovalID so the
// replay window in the approval flow recognises the prior grant. This is the
// ONLY coupling between the runtime and the governed dispatch path for resume;
// it cannot widen the envelope — it can only let an already-granted approval be
// recognised, never fabricate one.
type resumeApprovalIDKey struct{}

// WithResumeApprovalID returns a context carrying the granted approval id for
// the awaited tool call. An empty id is a no-op.
func WithResumeApprovalID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, resumeApprovalIDKey{}, id)
}

// ResumeApprovalIDFrom returns the granted approval id threaded by the runtime
// for the awaited tool call, or "" when the call is not a resume re-dispatch.
func ResumeApprovalIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(resumeApprovalIDKey{}).(string); ok {
		return v
	}
	return ""
}

// suspendInfo is the per-execution capture the awaited builtin records before
// aborting. Driven from the single interpreter goroutine; no locking needed.
type suspendInfo struct {
	callIndex  int
	approvalID string
	tool       string
}

// extractApprovalID pulls the approval id out of a governed dispatcher's
// approval-required error data (shape {"approval_id": "...", ...}). Returns ""
// when absent or unparsable — the suspend still proceeds, just without an id to
// thread (the handler treats an empty id as a non-resumable suspension).
func extractApprovalID(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var d struct {
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return ""
	}
	return d.ApprovalID
}
