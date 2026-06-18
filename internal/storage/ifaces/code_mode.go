package ifaces

import (
	"context"
	"errors"
	"time"
)

// CodeModeExecution is the audit/abuse-review record of one executeToolCode run.
// Every field is derived; the snippet itself is never stored here (only its
// SHA-256), and OutputRedacted is a redacted summary, never the full body.
type CodeModeExecution struct {
	TenantID       string
	ExecutionID    string
	SessionID      string
	StartedAt      string // RFC3339 UTC
	FinishedAt     string // empty while running / awaiting approval
	Status         string // CodeModeStatus* below
	SnippetSHA     string // sha-256 hex of the executed code
	ToolCalls      int
	TokensSavedEst int
	OutputRedacted string
	SpanID         string
}

// Execution status values.
const (
	CodeModeStatusRunning          = "running"
	CodeModeStatusCompleted        = "completed"
	CodeModeStatusFailed           = "failed"
	CodeModeStatusAwaitingApproval = "awaiting_approval"
)

// CodeModeContinuation is a suspended execution awaiting operator approval. It
// carries exactly what a deterministic replay needs: the immutable snapshot id,
// the snippet, the results of the tool calls that completed before the awaited
// one (indexed by ordinal), the awaited call index + approval id, and the
// frozen clock. CachedResults hold real tool outputs (replay fidelity demands
// it — a redacted value would change downstream control flow), so the row is
// sensitive at rest; it is tenant-scoped, single-use, and TTL-bounded to limit
// exposure (threat-model class C4/C6).
type CodeModeContinuation struct {
	TenantID           string
	ContinuationToken  string
	ExecutionID        string
	SessionID          string
	SnapshotID         string
	Code               string
	CachedResultsJSON  string // JSON array of result payloads for calls 0..AwaitingCallIndex-1
	AwaitingCallIndex  int
	AwaitingApprovalID string
	PrintBuffer        string // redacted print() snapshot (audit only)
	ClockUnix          int64  // frozen time.now() seconds
	CreatedAt          string // RFC3339 UTC
	ExpiresAt          string // RFC3339 UTC; CreatedAt + TTL
	ConsumedAt         string // empty until consumed
}

// Continuation sentinel errors. ConsumeContinuation returns exactly one of these
// (or nil) so the handler can map each to its precise code_mode.* guard.
var (
	// ErrContinuationNotFound — no row for (tenant, token). Also the result of a
	// cross-tenant probe: tenant B never sees tenant A's token (class C5).
	ErrContinuationNotFound = errors.New("storage: code mode continuation not found")
	// ErrContinuationConsumed — the token was already used (double_resume guard).
	ErrContinuationConsumed = errors.New("storage: code mode continuation already consumed")
	// ErrContinuationExpired — the token outlived its TTL (continuation_expired).
	ErrContinuationExpired = errors.New("storage: code mode continuation expired")
)

// CodeModeSummary is the rolled-up Code Mode activity for a tenant over a time
// window — the ROI numbers the /observability/code-mode dashboard renders.
type CodeModeSummary struct {
	// Executions is the total number of executeToolCode runs in the window.
	Executions int
	// ToolCalls is the total tool calls those runs issued.
	ToolCalls int
	// TokensSavedEst is the summed tokens-saved-vs-catalog estimate.
	TokensSavedEst int
	// ByStatus counts executions per terminal status (completed/failed/…).
	ByStatus map[string]int
}

// CodeModeStore persists Code Mode execution records and approval-suspension
// continuations. Every method is tenant-scoped (§6); the factory lives on the
// SQLite *DB and the dispatcher depends only on this interface (§4.4).
type CodeModeStore interface {
	// PutExecution inserts or replaces an execution record (keyed by
	// tenant+execution_id).
	PutExecution(ctx context.Context, e *CodeModeExecution) error
	// UpdateExecutionStatus sets status (+ finished_at, tool_calls, tokens_saved,
	// output_redacted) on an existing execution. finishedAt empty leaves it null.
	UpdateExecutionStatus(ctx context.Context, e *CodeModeExecution) error
	// ListExecutions returns a session's executions, most-recent first. sessionID
	// empty lists across the tenant. limit <= 0 means a sane default cap.
	ListExecutions(ctx context.Context, tenantID, sessionID string, limit int) ([]*CodeModeExecution, error)
	// SummarizeExecutions rolls up a tenant's executions started at or after
	// since (RFC3339 UTC; empty = all-time) into the dashboard ROI numbers.
	SummarizeExecutions(ctx context.Context, tenantID, since string) (*CodeModeSummary, error)

	// PutContinuation inserts a suspended-execution row.
	PutContinuation(ctx context.Context, c *CodeModeContinuation) error
	// ConsumeContinuation atomically marks the row consumed and returns it. It is
	// the single-use seam: a second call for the same token returns
	// ErrContinuationConsumed; an expired token returns ErrContinuationExpired
	// (and the row is deleted); a missing/cross-tenant token returns
	// ErrContinuationNotFound. now is the wall clock used for the TTL check.
	ConsumeContinuation(ctx context.Context, tenantID, token string, now time.Time) (*CodeModeContinuation, error)
	// DeleteExpiredContinuations removes rows whose expires_at < before (and any
	// already-consumed rows). Returns the count deleted. Driven by a sweeper.
	DeleteExpiredContinuations(ctx context.Context, before time.Time) (int, error)
}
