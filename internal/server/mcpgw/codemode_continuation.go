package mcpgw

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/runtime"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/telemetry"
)

// Code Mode continuation guard reasons. These travel in the JSON-RPC error's
// Data.code field (grouped under ErrCodeModeExecution) and in audit, naming the
// precise resume failure. Each is enforced by a test that must fail to break out
// (threat-model class C4).
const (
	reasonSnapshotDrifted     = "code_mode.snapshot_drifted"
	reasonDoubleResume        = "code_mode.double_resume"
	reasonContinuationExpired = "code_mode.continuation_expired"
	reasonContinuationUnknown = "code_mode.continuation_not_found"
)

// codeModeStatusAwaitingApproval is the structured-result status the meta-tool
// returns when an in-sandbox call suspended for approval. The client surfaces
// the approval and retries executeToolCode with the continuation_token.
const codeModeStatusAwaitingApproval = "approval_required"

// runCodeMode is the shared execution core for both a fresh executeToolCode and
// a resume. resume is nil for a fresh run; on a resume it carries the cached
// prior-call results + the granted approval id, and clock is the original frozen
// timestamp (so time.now() replays identically). executionID is empty for a
// fresh run (one is generated) and the original id on resume (so the record and
// any chained continuation stay tied to one logical execution).
func (d *Dispatcher) runCodeMode(ctx context.Context, sess *Session, code string, resume *runtime.ResumeState, clock time.Time, executionID string) (json.RawMessage, *protocol.Error) {
	ctx, span := telemetry.StartSpan(ctx, "code_mode.execution",
		telemetry.String(telemetry.AttrTenantID, sess.TenantID),
		telemetry.String(telemetry.AttrSessionID, sess.ID),
		telemetry.String(telemetry.AttrUserID, sess.UserID),
	)
	defer span.End()

	proj, snap, perr := d.projectCodeMode(ctx, sess)
	if perr != nil {
		return nil, perr
	}

	resuming := resume != nil
	if executionID == "" {
		executionID = newCodeModeToken()
	}
	snippetSHA := sha256Hex(code)
	d.recordExecution(ctx, sess, &ifaces.CodeModeExecution{
		ExecutionID: executionID,
		SessionID:   sess.ID,
		Status:      ifaces.CodeModeStatusRunning,
		SnippetSHA:  snippetSHA,
		SpanID:      executionID,
	})

	budget := runtime.DefaultBudget()
	if sess.CodeMode != nil && sess.CodeMode.MaxToolCalls > 0 {
		budget.MaxToolCalls = sess.CodeMode.MaxToolCalls
	}
	cfg := runtime.Config{
		Budget:     budget,
		Bindings:   toRuntimeBindings(proj.Tools),
		Dispatcher: sessionToolDispatcher{d: d, sess: sess},
		Redactor:   d.codeModeRedactor,
		Clock:      clock,
		Resume:     resume,
	}

	d.emitCodeMode(ctx, sess, audit.EventCodeModeExecStarted, map[string]any{
		"code_bytes": len(code),
		"resumed":    resuming,
	})

	res, runErr := runtime.Execute(ctx, code, cfg)
	if runErr != nil {
		// Approval suspension is a control-flow signal, not a failure: persist the
		// continuation and return a resumable status.
		var susp *runtime.Suspension
		if errors.As(runErr, &susp) {
			return d.suspendCodeMode(ctx, sess, code, snippetSHA, executionID, snap, susp)
		}
		_ = telemetry.RecordErr(span, runErr)
		d.emitCodeMode(ctx, sess, audit.EventCodeModeExecFailed, codeModeErrPayload(runErr))
		d.recordExecution(ctx, sess, &ifaces.CodeModeExecution{
			ExecutionID: executionID,
			SessionID:   sess.ID,
			Status:      ifaces.CodeModeStatusFailed,
			SnippetSHA:  snippetSHA,
			FinishedAt:  nowRFC3339(),
			SpanID:      executionID,
		})
		return nil, codeModeErrorToProtocol(runErr)
	}

	saved := codemode.EstimateTokensSaved(snap, res.ToolCalls, len(code), len(res.Result))
	span.SetAttributes(
		telemetry.Int("code_mode.tool_calls", res.ToolCalls),
		telemetry.Int("code_mode.tokens_saved_est", saved),
	)
	d.emitCodeMode(ctx, sess, audit.EventCodeModeExecCompleted, map[string]any{
		"tool_calls":       res.ToolCalls,
		"tokens_saved_est": saved,
		"duration_ms":      res.Duration.Milliseconds(),
		"output_truncated": res.OutputTruncated,
		"resumed":          resuming,
	})
	d.recordExecution(ctx, sess, &ifaces.CodeModeExecution{
		ExecutionID:    executionID,
		SessionID:      sess.ID,
		Status:         ifaces.CodeModeStatusCompleted,
		SnippetSHA:     snippetSHA,
		ToolCalls:      res.ToolCalls,
		TokensSavedEst: saved,
		FinishedAt:     nowRFC3339(),
		SpanID:         executionID,
	})

	structured, _ := json.Marshal(map[string]any{
		"result":           res.Result,
		"output":           res.Output,
		"output_truncated": res.OutputTruncated,
		"tool_calls":       res.ToolCalls,
		"tokens_saved_est": saved,
		"duration_ms":      res.Duration.Milliseconds(),
	})
	return metaResult(structured, res.Output)
}

// suspendCodeMode persists a continuation for a suspended execution and returns
// the resumable status to the client. When continuations can't be persisted
// (no store, or the suspend carried no approval id to thread on resume), it
// falls back to the plain approval_required error so the caller still learns
// approval is needed — it just can't auto-resume.
func (d *Dispatcher) suspendCodeMode(ctx context.Context, sess *Session, code, snippetSHA, executionID string, snap *snapshots.Snapshot, susp *runtime.Suspension) (json.RawMessage, *protocol.Error) {
	snapshotID := ""
	if snap != nil {
		snapshotID = snap.ID
	}
	if d.codeModeStore == nil || susp.ApprovalID == "" || snapshotID == "" {
		d.emitCodeMode(ctx, sess, audit.EventCodeModeExecFailed, map[string]any{"code": runtime.CodeApprovalRequired, "detail": susp.Tool})
		return nil, protocol.NewError(protocol.ErrApprovalRequired, "approval_required", map[string]any{
			"tool":        susp.Tool,
			"approval_id": susp.ApprovalID,
		})
	}

	cachedJSON, err := json.Marshal(susp.CachedResults)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, "code mode: serialize continuation", nil)
	}
	token := newCodeModeToken()

	// The execution row must exist (FK target) and reflect the suspension.
	d.recordExecution(ctx, sess, &ifaces.CodeModeExecution{
		ExecutionID: executionID,
		SessionID:   sess.ID,
		Status:      ifaces.CodeModeStatusAwaitingApproval,
		SnippetSHA:  snippetSHA,
		ToolCalls:   susp.ToolCalls,
		SpanID:      executionID,
	})

	cont := &ifaces.CodeModeContinuation{
		TenantID:           sess.TenantID,
		ContinuationToken:  token,
		ExecutionID:        executionID,
		SessionID:          sess.ID,
		SnapshotID:         snapshotID,
		Code:               code,
		CachedResultsJSON:  string(cachedJSON),
		AwaitingCallIndex:  susp.CallIndex,
		AwaitingApprovalID: susp.ApprovalID,
		PrintBuffer:        susp.PrintBuffer, // already redacted by the runtime
		ClockUnix:          susp.Clock.Unix(),
	}
	if err := d.codeModeStore.PutContinuation(ctx, cont); err != nil {
		d.log.Warn("code mode: persist continuation failed", "session_id", sess.ID, "err", err)
		return nil, protocol.NewError(protocol.ErrInternalError, "code mode: persist continuation", nil)
	}

	d.emitCodeMode(ctx, sess, audit.EventCodeModeExecSuspended, map[string]any{
		"tool":              susp.Tool,
		"awaiting_call_idx": susp.CallIndex,
		"tool_calls":        susp.ToolCalls,
	})

	structured, _ := json.Marshal(map[string]any{
		"status":             codeModeStatusAwaitingApproval,
		"approval_id":        susp.ApprovalID,
		"continuation_token": token,
		"tool":               susp.Tool,
		"tool_calls":         susp.ToolCalls,
	})
	return metaResult(structured, "approval required for "+susp.Tool)
}

// resumeCodeMode reloads a suspended execution by its continuation token and
// replays it. It enforces the C4 guards in order: single-use (double_resume),
// TTL (continuation_expired), cross-tenant/unknown (continuation_not_found), and
// snapshot drift (snapshot_drifted). Only after all pass does it replay.
func (d *Dispatcher) resumeCodeMode(ctx context.Context, sess *Session, token string) (json.RawMessage, *protocol.Error) {
	if d.codeModeStore == nil {
		return nil, codeModeGuardError(reasonContinuationUnknown, "code mode continuations are not supported on this build")
	}
	cont, err := d.codeModeStore.ConsumeContinuation(ctx, sess.TenantID, token, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, ifaces.ErrContinuationConsumed):
			return nil, codeModeGuardError(reasonDoubleResume, "continuation already used")
		case errors.Is(err, ifaces.ErrContinuationExpired):
			return nil, codeModeGuardError(reasonContinuationExpired, "continuation expired")
		case errors.Is(err, ifaces.ErrContinuationNotFound):
			return nil, codeModeGuardError(reasonContinuationUnknown, "unknown continuation token")
		default:
			return nil, protocol.NewError(protocol.ErrInternalError, "code mode: load continuation", nil)
		}
	}

	// Snapshot drift: the pinned snapshot must still be the session's active one,
	// or the replay would bind different tools and is unsafe. Fail closed.
	_, snap, perr := d.projectCodeMode(ctx, sess)
	if perr != nil {
		return nil, perr
	}
	currentSnapshotID := ""
	if snap != nil {
		currentSnapshotID = snap.ID
	}
	if currentSnapshotID != cont.SnapshotID {
		d.emitCodeMode(ctx, sess, audit.EventCodeModeExecFailed, map[string]any{
			"code":            reasonSnapshotDrifted,
			"pinned_snapshot": cont.SnapshotID,
		})
		return nil, codeModeGuardError(reasonSnapshotDrifted, "snapshot changed since suspension; re-run required")
	}

	var cached []json.RawMessage
	if cont.CachedResultsJSON != "" {
		if uerr := json.Unmarshal([]byte(cont.CachedResultsJSON), &cached); uerr != nil {
			return nil, protocol.NewError(protocol.ErrInternalError, "code mode: decode cached results", nil)
		}
	}
	resume := &runtime.ResumeState{CachedResults: cached, ApprovalID: cont.AwaitingApprovalID}
	clock := time.Unix(cont.ClockUnix, 0).UTC()
	return d.runCodeMode(ctx, sess, cont.Code, resume, clock, cont.ExecutionID)
}

// recordExecution upserts an execution row, nil-safe on a missing store. insert
// distinguishes the initial running row (which the continuation FK needs) from a
// status update; both go through the upserting PutExecution.
func (d *Dispatcher) recordExecution(ctx context.Context, sess *Session, e *ifaces.CodeModeExecution) {
	if d.codeModeStore == nil {
		return
	}
	e.TenantID = sess.TenantID
	if e.StartedAt == "" {
		e.StartedAt = nowRFC3339()
	}
	if err := d.codeModeStore.PutExecution(ctx, e); err != nil {
		d.log.Warn("code mode: record execution failed", "session_id", sess.ID, "err", err)
	}
}

// codeModeGuardError builds a JSON-RPC error for a continuation guard trip. The
// specific code_mode.* reason rides in Data.code under the execution error code.
func codeModeGuardError(reason, msg string) *protocol.Error {
	return protocol.NewError(protocol.ErrCodeModeExecution, "code mode: "+msg, map[string]any{"code": reason})
}

// newCodeModeToken returns a random 128-bit URL-safe token for execution ids and
// continuation tokens. crypto/rand failure is fatal-by-construction here; the
// caller treats an empty token as an internal error.
func newCodeModeToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
