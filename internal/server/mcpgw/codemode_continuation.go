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
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/catalog"
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
func (d *Dispatcher) runCodeMode(ctx context.Context, sess *Session, code string, resume *runtime.ResumeState, clock time.Time, executionID string, execApproved bool) (json.RawMessage, *protocol.Error) {
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
	snippetSHA := sha256Hex(code)

	// Code Mode policy gate — only on a genuinely fresh execution (a resume, or a
	// run already approved at the execution level, has passed it). Returns a deny
	// or an approval_required suspension; a generated executionID is reused.
	if !resuming && !execApproved {
		if done, resp, gErr := d.gateExecution(ctx, sess, code, snippetSHA, &executionID, snap); done {
			return resp, gErr
		}
	}
	if executionID == "" {
		executionID = newCodeModeToken()
	}
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
	// The policy tool-call ceiling lowers (never raises) the session's request.
	budget.MaxToolCalls = d.codeModePolicy.EffectiveMaxToolCalls(budget.MaxToolCalls)
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
		// deny_on_unsafe_starlark: a static-gate rejection is always rejected; this
		// additionally records it as an audited policy denial for abuse tracking.
		if d.codeModePolicy.DenyUnsafeStarlark {
			var se *runtime.SandboxError
			if errors.As(runErr, &se) && se.Code == runtime.CodeUnsafeCall {
				d.emitCodeMode(ctx, sess, audit.EventCodeModeUnsafeDenied, map[string]any{"detail": se.Detail})
			}
		}
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

// execApprovalCallIndex is the AwaitingCallIndex sentinel marking a continuation
// that gates the WHOLE execution behind approval (require_approval_on_executeToolCode),
// as opposed to an in-sandbox tool call awaiting approval (index >= 0).
const execApprovalCallIndex = -1

// Code Mode policy guard reasons (in addition to the continuation reasons above).
const (
	reasonApprovalUnavailable = "code_mode.approval_unavailable"
	reasonExecutionDenied     = "code_mode.execution_denied"
)

// gateExecution applies the Code Mode policy to a fresh execution before it runs.
// It returns done=true with a response/error when the call must stop here — a
// policy deny, or an approval_required suspension when the whole execution needs
// sign-off. done=false means proceed to run the snippet. executionID is filled
// in when a suspension needs to reference the execution row.
func (d *Dispatcher) gateExecution(ctx context.Context, sess *Session, code, snippetSHA string, executionID *string, snap *snapshots.Snapshot) (bool, json.RawMessage, *protocol.Error) {
	level := catalog.BindingServer
	if sess.CodeMode != nil {
		level = sess.CodeMode.BindingLevel
	}
	dec := d.codeModePolicy.Evaluate(codemode.EvalInput{
		Enabled:      true,
		BindingLevel: string(level),
		CodeBytes:    len(code),
		IsResume:     false,
	})
	if dec.Deny {
		d.emitCodeMode(ctx, sess, audit.EventCodeModeExecFailed, map[string]any{"code": dec.Reason})
		return true, nil, codeModeGuardError(dec.Reason, "code mode policy denied")
	}
	if !dec.RequireApproval {
		return false, nil, nil
	}
	// Whole-execution approval gate. Without an approval flow we cannot grant it,
	// so fail closed rather than run unapproved.
	if d.policy == nil {
		return true, nil, codeModeGuardError(reasonApprovalUnavailable, "approval required but no approval flow configured")
	}
	if *executionID == "" {
		*executionID = newCodeModeToken()
	}
	out, err := d.policy.ApproveExecution(ctx, sess, execApprovalArgs(snippetSHA), "")
	if err != nil {
		return true, nil, codeModeGuardError(reasonApprovalUnavailable, err.Error())
	}
	switch {
	case out.Approved():
		return false, nil, nil // approved inline (elicitation) → proceed
	case out.FallbackRequired():
		resp, perr := d.suspendForExecApproval(ctx, sess, code, snippetSHA, *executionID, snap, out.Approval.ID)
		return true, resp, perr
	default: // denied / expired
		d.emitCodeMode(ctx, sess, audit.EventCodeModeExecFailed, map[string]any{"code": reasonExecutionDenied})
		return true, nil, codeModeGuardError(reasonExecutionDenied, "execution approval denied")
	}
}

// suspendForExecApproval persists an execution-level continuation (no in-sandbox
// cache) and returns the resumable approval_required status. The resume verifies
// the whole-execution approval and runs the snippet fresh.
func (d *Dispatcher) suspendForExecApproval(ctx context.Context, sess *Session, code, snippetSHA, executionID string, snap *snapshots.Snapshot, approvalID string) (json.RawMessage, *protocol.Error) {
	snapshotID := ""
	if snap != nil {
		snapshotID = snap.ID
	}
	if d.codeModeStore == nil || approvalID == "" || snapshotID == "" {
		return nil, protocol.NewError(protocol.ErrApprovalRequired, "approval_required", map[string]any{
			"tool":        metaExecuteToolCode,
			"approval_id": approvalID,
		})
	}
	token := newCodeModeToken()
	d.recordExecution(ctx, sess, &ifaces.CodeModeExecution{
		ExecutionID: executionID,
		SessionID:   sess.ID,
		Status:      ifaces.CodeModeStatusAwaitingApproval,
		SnippetSHA:  snippetSHA,
		SpanID:      executionID,
	})
	cont := &ifaces.CodeModeContinuation{
		TenantID:           sess.TenantID,
		ContinuationToken:  token,
		ExecutionID:        executionID,
		SessionID:          sess.ID,
		SnapshotID:         snapshotID,
		Code:               code,
		CachedResultsJSON:  "[]",
		AwaitingCallIndex:  execApprovalCallIndex,
		AwaitingApprovalID: approvalID,
		ClockUnix:          time.Now().Unix(),
	}
	if err := d.codeModeStore.PutContinuation(ctx, cont); err != nil {
		d.log.Warn("code mode: persist exec-approval continuation failed", "session_id", sess.ID, "err", err)
		return nil, protocol.NewError(protocol.ErrInternalError, "code mode: persist continuation", nil)
	}
	d.emitCodeMode(ctx, sess, audit.EventCodeModeExecSuspended, map[string]any{
		"tool":              metaExecuteToolCode,
		"awaiting_call_idx": execApprovalCallIndex,
	})
	structured, _ := json.Marshal(map[string]any{
		"status":             codeModeStatusAwaitingApproval,
		"approval_id":        approvalID,
		"continuation_token": token,
		"tool":               metaExecuteToolCode,
	})
	return metaResult(structured, "approval required to run this code mode execution")
}

// execApprovalArgs builds the stable, compact arguments for a whole-execution
// approval — the snippet digest, so the original gate and the resume hash to the
// same value and the replay window recognises the grant.
func execApprovalArgs(snippetSHA string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"snippet_sha": snippetSHA})
	return b
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

	// An execution-level continuation (the require_approval_on_executeToolCode
	// gate) carries no in-sandbox cache; it re-verifies the whole-execution
	// approval and then runs the snippet fresh.
	if cont.AwaitingCallIndex == execApprovalCallIndex {
		return d.resumeExecApproval(ctx, sess, cont)
	}

	var cached []json.RawMessage
	if cont.CachedResultsJSON != "" {
		if uerr := json.Unmarshal([]byte(cont.CachedResultsJSON), &cached); uerr != nil {
			return nil, protocol.NewError(protocol.ErrInternalError, "code mode: decode cached results", nil)
		}
	}
	resume := &runtime.ResumeState{CachedResults: cached, ApprovalID: cont.AwaitingApprovalID}
	clock := time.Unix(cont.ClockUnix, 0).UTC()
	return d.runCodeMode(ctx, sess, cont.Code, resume, clock, cont.ExecutionID, true)
}

// resumeExecApproval re-verifies a whole-execution approval (the
// require_approval_on_executeToolCode gate) and, once granted, runs the snippet
// fresh. The approval is re-checked through the identical flow + replay window
// the gate opened; only a genuinely-granted approval for the same snippet digest
// lets it through.
func (d *Dispatcher) resumeExecApproval(ctx context.Context, sess *Session, cont *ifaces.CodeModeContinuation) (json.RawMessage, *protocol.Error) {
	if d.policy == nil {
		return nil, codeModeGuardError(reasonApprovalUnavailable, "approval flow not configured")
	}
	out, err := d.policy.ApproveExecution(ctx, sess, execApprovalArgs(sha256Hex(cont.Code)), cont.AwaitingApprovalID)
	if err != nil {
		return nil, codeModeGuardError(reasonApprovalUnavailable, err.Error())
	}
	if !out.Approved() {
		d.emitCodeMode(ctx, sess, audit.EventCodeModeExecFailed, map[string]any{"code": reasonExecutionDenied})
		return nil, codeModeGuardError(reasonExecutionDenied, "execution approval not granted")
	}
	clock := time.Unix(cont.ClockUnix, 0).UTC()
	return d.runCodeMode(ctx, sess, cont.Code, nil, clock, cont.ExecutionID, true)
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
