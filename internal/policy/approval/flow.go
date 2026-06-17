// Package approval owns the gateway's approval flow: persist a pending
// approval, attempt to elicit a decision via the northbound MCP channel,
// fall back to a structured JSON-RPC error when the host doesn't support
// elicitation, and let operators resolve approvals manually via REST.
//
// Wire diagram:
//
//	dispatcher ─► Flow.Run ─┬─► Store.Insert (pending row)
//	                        ├─► Audit.Emit (approval.pending)
//	                        ├─► (if HasElicitation) Sender.Elicit ─► client UI
//	                        └─► Outcome (approved | denied | timeout | fallback_required)
package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/policy"
)

// Status values stored in the approvals table.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusDenied   = "denied"
	StatusExpired  = "expired"
)

// Approval mirrors one row of the `approvals` table.
type Approval struct {
	ID          string
	TenantID    string
	SessionID   string
	UserID      string
	Tool        string
	ArgsSummary string
	RiskClass   string
	Status      string
	CreatedAt   time.Time
	DecidedAt   *time.Time
	ExpiresAt   time.Time
	Metadata    map[string]any
}

// Outcome is what the dispatcher gets back. Approved/Denied/Expired are
// terminal; FallbackRequired tells the dispatcher to emit -32001 instead.
type Outcome struct {
	Approval *Approval
	Decision string // "approved" | "denied" | "expired" | "fallback_required"
}

// Approved reports whether the dispatcher should let the call through.
func (o Outcome) Approved() bool { return o.Decision == StatusApproved }

// FallbackRequired reports whether the dispatcher should emit a
// -32001 approval_required error instead of waiting for a decision.
func (o Outcome) FallbackRequired() bool { return o.Decision == "fallback_required" }

// Store is the persistence seam. The SQLite implementation lives in
// internal/storage/sqlite/approval_store.go.
type Store interface {
	Insert(ctx context.Context, a *Approval) error
	UpdateStatus(ctx context.Context, tenantID, id, status string, decidedAt time.Time) error
	Get(ctx context.Context, tenantID, id string) (*Approval, error)
	ListPending(ctx context.Context, tenantID string) ([]*Approval, error)
	ExpireOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}

// Sender abstracts the northbound transport's server-initiated channel.
// The Phase 5 transport layer registers a Sender that ships
// elicitation/create requests and waits for the matching response.
type Sender interface {
	Elicit(ctx context.Context, sessionID string, params protocol.ElicitationCreateParams, timeout time.Duration) (*protocol.ElicitationCreateResult, error)
}

// SessionLookup returns the active session record for an id. Used to
// check ClientCaps.HasElicitation.
type SessionLookup interface {
	HasElicitation(sessionID string) bool
}

// CallContext is the snapshot of the tools/call params the flow needs to
// build a useful elicitation prompt + audit row. The dispatcher fills it.
type CallContext struct {
	Tool       string
	Arguments  json.RawMessage
	SkillID    string
	RiskClass  string
	ApprovalID string // optional pre-generated id; flow generates one if empty
}

// Flow orchestrates the approval lifecycle. Cheap to construct; safe for
// concurrent use.
type Flow struct {
	store    Store
	sender   Sender
	sessions SessionLookup
	emitter  audit.Emitter
	log      *slog.Logger

	idMu sync.Mutex
	idC  uint64
}

// New constructs a Flow. emitter may be NopEmitter when audit isn't yet
// wired (early bring-up only).
func New(store Store, sender Sender, sessions SessionLookup, emitter audit.Emitter, log *slog.Logger) *Flow {
	if log == nil {
		log = slog.Default()
	}
	if emitter == nil {
		emitter = audit.NopEmitter{}
	}
	return &Flow{
		store:    store,
		sender:   sender,
		sessions: sessions,
		emitter:  emitter,
		log:      log,
	}
}

// Run is the main entry point. The dispatcher passes the policy decision
// (which contains tool, risk class, timeout) and the per-call context.
func (f *Flow) Run(ctx context.Context, tenantID, sessionID, userID string, dec policy.Decision, call CallContext) (Outcome, error) {
	if f == nil || f.store == nil {
		return Outcome{}, errors.New("approval: flow not configured")
	}
	now := time.Now().UTC()

	// Replay window (CLAUDE.md §7.4): when the caller supplies an approval id
	// for an approval that was already decided for THIS tool with THESE exact
	// arguments, honour the prior decision instead of opening a fresh pending
	// row. This is what lets the Code Mode continuation flow resume the awaited
	// tool call — the runtime threads the granted approval id, the call
	// re-dispatches through the identical governed envelope, and the gate
	// recognises the grant rather than prompting again. The match is strict
	// (same tool + same args summary) and fails closed: a mismatch, a pending
	// row, or a lookup error falls through to the normal pending flow, never to
	// an unchecked allow.
	if call.ApprovalID != "" {
		if out, ok := f.replayDecision(ctx, tenantID, dec, call); ok {
			return out, nil
		}
	}

	timeout := dec.ApprovalTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	a := &Approval{
		ID:          orGenerateID(call.ApprovalID, &f.idMu, &f.idC),
		TenantID:    tenantID,
		SessionID:   sessionID,
		UserID:      userID,
		Tool:        dec.Tool,
		ArgsSummary: summarizeArgs(call.Arguments),
		RiskClass:   call.RiskClass,
		Status:      StatusPending,
		CreatedAt:   now,
		ExpiresAt:   now.Add(timeout),
		Metadata: map[string]any{
			"skill_id": call.SkillID,
			// args_hash is a non-lossy digest of the FULL arguments. The replay
			// window compares it (not the 1024-byte display summary), so an
			// approval can never be replayed onto arguments that merely share a
			// 1024-byte prefix (red-team round 2, C4-1).
			"args_hash": argsHash(call.Arguments),
		},
	}
	if err := f.store.Insert(ctx, a); err != nil {
		return Outcome{}, fmt.Errorf("approval: insert: %w", err)
	}
	f.emit(ctx, audit.EventApprovalPending, a, nil)

	hasElicit := f.sessions != nil && f.sessions.HasElicitation(sessionID)
	if !hasElicit || f.sender == nil {
		// Fallback path: dispatcher emits -32001 with the approval id; the
		// pending row stays pending until an operator resolves it.
		return Outcome{Approval: a, Decision: "fallback_required"}, nil
	}

	res, err := f.sender.Elicit(ctx, sessionID, buildElicitationParams(a, dec), timeout)
	decided := time.Now().UTC()
	if err != nil {
		// Timeout / disconnect / IdP error → mark expired so the next
		// retry doesn't reuse a stale row.
		_ = f.store.UpdateStatus(ctx, tenantID, a.ID, StatusExpired, decided)
		a.Status = StatusExpired
		a.DecidedAt = &decided
		f.emit(ctx, audit.EventApprovalExpired, a, map[string]any{"reason": err.Error()})
		return Outcome{Approval: a, Decision: StatusExpired}, nil
	}

	switch res.Action {
	case protocol.ElicitActionAccept:
		approve, _ := extractApprovePayload(res.Content)
		status := StatusDenied
		if approve {
			status = StatusApproved
		}
		_ = f.store.UpdateStatus(ctx, tenantID, a.ID, status, decided)
		a.Status = status
		a.DecidedAt = &decided
		f.emit(ctx, audit.EventApprovalDecided, a, map[string]any{"decision": status})
		return Outcome{Approval: a, Decision: status}, nil
	case protocol.ElicitActionReject, protocol.ElicitActionCancel:
		_ = f.store.UpdateStatus(ctx, tenantID, a.ID, StatusDenied, decided)
		a.Status = StatusDenied
		a.DecidedAt = &decided
		f.emit(ctx, audit.EventApprovalDecided, a, map[string]any{"decision": StatusDenied, "client_action": res.Action})
		return Outcome{Approval: a, Decision: StatusDenied}, nil
	default:
		_ = f.store.UpdateStatus(ctx, tenantID, a.ID, StatusExpired, decided)
		a.Status = StatusExpired
		a.DecidedAt = &decided
		f.emit(ctx, audit.EventApprovalExpired, a, map[string]any{"reason": "unknown_action", "action": res.Action})
		return Outcome{Approval: a, Decision: StatusExpired}, nil
	}
}

// replayDecision returns the outcome for an already-decided approval matching
// the supplied id, or ok=false to fall through to the normal pending flow. It
// only ever SHORTENS the flow — it can return approved/denied/expired for a row
// that already reached that state, never create or grant a new approval.
func (f *Flow) replayDecision(ctx context.Context, tenantID string, dec policy.Decision, call CallContext) (Outcome, bool) {
	existing, err := f.store.Get(ctx, tenantID, call.ApprovalID)
	if err != nil || existing == nil {
		return Outcome{}, false // unknown id → fail closed to the pending flow
	}
	// Strict three-way identity (CLAUDE.md §7 #4): the prior approval must be for
	// the same tool, the same FULL arguments (compared by non-lossy hash, not the
	// truncated display summary), and the same skill id. Any mismatch — or a row
	// from before this guard existed (no args_hash) — fails closed to the pending
	// flow. This stops a continuation from replaying one approval onto a different
	// call, different arguments, or a different skill (red-team round 2, C4-1/C4-2).
	storedHash, _ := existing.Metadata["args_hash"].(string)
	storedSkill, _ := existing.Metadata["skill_id"].(string)
	if existing.Tool != dec.Tool || storedHash == "" || storedHash != argsHash(call.Arguments) || storedSkill != call.SkillID {
		return Outcome{}, false
	}
	switch existing.Status {
	case StatusApproved:
		f.emit(ctx, audit.EventApprovalReplayed, existing, map[string]any{"tool": existing.Tool})
		return Outcome{Approval: existing, Decision: StatusApproved}, true
	case StatusDenied:
		return Outcome{Approval: existing, Decision: StatusDenied}, true
	case StatusExpired:
		return Outcome{Approval: existing, Decision: StatusExpired}, true
	default:
		// Still pending — the operator hasn't acted; let the normal flow run so
		// the caller gets a fresh fallback/elicitation rather than a stale row.
		return Outcome{}, false
	}
}

// ResolveManually is used by the REST endpoint when an operator approves
// or denies a pending approval out-of-band. Returns the updated row.
func (f *Flow) ResolveManually(ctx context.Context, tenantID, id, status, actorUserID string) (*Approval, error) {
	if status != StatusApproved && status != StatusDenied {
		return nil, fmt.Errorf("approval: invalid status %q", status)
	}
	now := time.Now().UTC()
	if err := f.store.UpdateStatus(ctx, tenantID, id, status, now); err != nil {
		return nil, err
	}
	a, err := f.store.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	f.emit(ctx, audit.EventApprovalDecided, a, map[string]any{
		"decision":   status,
		"decided_by": actorUserID,
		"channel":    "manual",
	})
	return a, nil
}

// Sweep marks expired pending approvals. Operators call this from a
// periodic ticker; the Phase 5 cmd/portico wiring schedules it once a
// minute.
func (f *Flow) Sweep(ctx context.Context) (int, error) {
	if f.store == nil {
		return 0, nil
	}
	return f.store.ExpireOlderThan(ctx, time.Now().UTC())
}

func (f *Flow) emit(ctx context.Context, evType string, a *Approval, extra map[string]any) {
	payload := map[string]any{
		"approval_id": a.ID,
		"tool":        a.Tool,
		"risk_class":  a.RiskClass,
		"skill_id":    a.Metadata["skill_id"],
	}
	for k, v := range extra {
		payload[k] = v
	}
	f.emitter.Emit(ctx, audit.Event{
		Type:      evType,
		TenantID:  a.TenantID,
		SessionID: a.SessionID,
		UserID:    a.UserID,
		Payload:   payload,
	})
}

func summarizeArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	const max = 1024
	if len(args) > max {
		return string(args[:max]) + "…"
	}
	return string(args)
}

// argsHash returns a SHA-256 hex digest of the arguments, canonicalised
// (re-marshalled to normalise key order / whitespace) when they are valid JSON
// so the same logical arguments hash identically regardless of serialisation.
// Unlike summarizeArgs it is non-lossy: two payloads differ in their hash even
// if they share a 1024-byte prefix. Used only for the replay-window identity
// check; the human-readable ArgsSummary is unchanged.
func argsHash(args json.RawMessage) string {
	canonical := []byte(args)
	var v any
	if err := json.Unmarshal(args, &v); err == nil {
		if b, mErr := json.Marshal(v); mErr == nil {
			canonical = b
		}
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// extractApprovePayload reads `{"approve": bool}` from the elicitation
// content. Treats anything other than `true` as denial.
func extractApprovePayload(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	var p struct {
		Approve bool `json:"approve"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return false, err
	}
	return p.Approve, nil
}

// buildElicitationParams renders the user-facing prompt from the pending
// approval row.
func buildElicitationParams(a *Approval, dec policy.Decision) protocol.ElicitationCreateParams {
	schema := json.RawMessage(`{
  "type": "object",
  "properties": {
    "approve": {"type": "boolean", "title": "Approve this action?"},
    "note":    {"type": "string", "title": "Optional reason"}
  },
  "required": ["approve"]
}`)
	meta, _ := json.Marshal(map[string]any{
		"portico": map[string]any{
			"approval_id":  a.ID,
			"tool":         a.Tool,
			"risk_class":   a.RiskClass,
			"skill_id":     a.Metadata["skill_id"],
			"args_summary": a.ArgsSummary,
			"expires_at":   a.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
	msg := fmt.Sprintf("Approve calling %s? Risk: %s.", dec.Tool, dec.RiskClass)
	return protocol.ElicitationCreateParams{
		Message:         msg,
		RequestedSchema: schema,
		Meta:            meta,
	}
}

func orGenerateID(supplied string, mu *sync.Mutex, ctr *uint64) string {
	if supplied != "" {
		return supplied
	}
	// Time-based id keeps tests deterministic without bringing in an
	// external dep here. Production callers usually supply call.ApprovalID.
	mu.Lock()
	*ctr++
	c := *ctr
	mu.Unlock()
	return fmt.Sprintf("apr_%d_%d", time.Now().UnixNano(), c)
}
