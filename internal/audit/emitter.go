// Package audit owns the gateway's tamper-evident-ish event log: every
// tool call, policy decision, approval transition, credential injection,
// and vault mutation lands here so operators can answer "who did what to
// what tenant and when" without having to scrape logs.
//
// The package ships three pieces:
//
//   - Emitter: the contract callers see. FanoutEmitter wraps multiple sinks
//     so the runtime can push to slog and the SQLite store with one call.
//   - Store: the persistent SQLite-backed sink. Bounded buffer, drop-oldest
//     on overflow with a self-recording `audit.dropped` event.
//   - Redactor: regex-based scrubbing that runs on every payload before
//     persistence so the on-disk log never carries raw bearer tokens or
//     similar credentials, even if a caller forgets to pre-summarise.
package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Common event types. Other packages reference these constants instead of
// raw strings. The values look credential-shaped to gosec (they contain
// the word "credential" / "vault"); the rule is silenced inline because
// these are event labels, not secrets.
const (
	EventToolCallStart    = "tool_call.start"
	EventToolCallComplete = "tool_call.complete"
	EventToolCallFailed   = "tool_call.failed"
	EventPolicyAllowed    = "policy.allowed"
	EventPolicyDenied     = "policy.denied"
	EventApprovalPending  = "approval.pending"
	EventApprovalDecided  = "approval.decided"
	EventApprovalExpired  = "approval.expired"
	//nolint:gosec // event label, not a credential
	EventCredentialInjected = "credential.injected"
	//nolint:gosec // event label, not a credential
	EventCredentialExchangeOK = "credential.exchange.success"
	//nolint:gosec // event label, not a credential
	EventCredentialExchangeNG = "credential.exchange.failed"
	EventVaultGet             = "vault.get"
	EventVaultPut             = "vault.put"
	EventVaultDelete          = "vault.delete"
	EventAuditDropped         = "audit.dropped"

	// Phase 9 — Console CRUD events. Each emits with tenant_id, the
	// actor user_id, and a redacted payload carrying before/after diffs.
	EventServerCreated   = "server.created"
	EventServerUpdated   = "server.updated"
	EventServerDeleted   = "server.deleted"
	EventServerRestarted = "server.restarted"
	EventTenantCreated   = "tenant.created"
	EventTenantUpdated   = "tenant.updated"
	EventTenantArchived  = "tenant.archived"
	EventTenantPurged    = "tenant.purged"
	//nolint:gosec // event label, not a credential
	EventSecretCreated = "secret.created"
	//nolint:gosec // event label, not a credential
	EventSecretUpdated = "secret.updated"
	//nolint:gosec // event label, not a credential
	EventSecretDeleted = "secret.deleted"
	//nolint:gosec // event label, not a credential
	EventSecretRotated = "secret.rotated"
	//nolint:gosec // event label, not a credential
	EventSecretRevealIssued = "secret.reveal.issued"
	//nolint:gosec // event label, not a credential
	EventSecretRevealConsumed = "secret.reveal.consumed"
	//nolint:gosec // event label, not a credential
	EventVaultRotateRoot = "vault.rotate_root"
	//nolint:gosec // event label, not a credential
	EventVaultRotateRootAborted = "vault.rotate_root.aborted"
	EventPolicyRuleChanged      = "policy.rule_changed"
	EventPolicyRuleDeleted      = "policy.rule_deleted"
	EventPolicyDryRun           = "policy.dry_run"
)

// Event is one structured log entry. Payload is an arbitrary JSON-serialisable
// map; the Redactor scrubs known credential shapes before persistence.
//
// TenantID is required by every consumer; SessionID/UserID may be empty for
// system-originated events (e.g. vault.put from a CLI). OccurredAt defaults
// to the emit-site clock when zero.
type Event struct {
	Type       string         `json:"type"`
	TenantID   string         `json:"tenant_id"`
	SessionID  string         `json:"session_id,omitempty"`
	UserID     string         `json:"user_id,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
	TraceID    string         `json:"trace_id,omitempty"`
	SpanID     string         `json:"span_id,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// Emitter is the seam every event-producing call site depends on. The
// runtime wires a FanoutEmitter pointing at the SQLite store and the
// process logger; tests can use a SliceEmitter or NopEmitter.
type Emitter interface {
	Emit(ctx context.Context, e Event)
}

// FanoutEmitter forwards every event to all configured sinks. Sink errors
// are absorbed (audit must not break the request path); callers wanting
// guaranteed durability should pass a SyncEmitter that reports back.
type FanoutEmitter struct {
	mu    sync.RWMutex
	sinks []Emitter
}

// NewFanoutEmitter constructs a FanoutEmitter from the provided sinks. The
// slice is copied so subsequent mutations to the caller's slice do not
// affect dispatch.
func NewFanoutEmitter(sinks ...Emitter) *FanoutEmitter {
	cp := make([]Emitter, 0, len(sinks))
	cp = append(cp, sinks...)
	return &FanoutEmitter{sinks: cp}
}

// Add registers another sink at runtime (e.g. when the SQLite store comes
// up after the slog logger).
func (f *FanoutEmitter) Add(s Emitter) {
	if s == nil {
		return
	}
	f.mu.Lock()
	f.sinks = append(f.sinks, s)
	f.mu.Unlock()
}

// Emit fans out to every sink in registration order.
func (f *FanoutEmitter) Emit(ctx context.Context, e Event) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	f.mu.RLock()
	sinks := append([]Emitter(nil), f.sinks...)
	f.mu.RUnlock()
	for _, s := range sinks {
		s.Emit(ctx, e)
	}
}

// NopEmitter discards every event. Useful in tests that don't care.
type NopEmitter struct{}

// Emit on NopEmitter is a no-op.
func (NopEmitter) Emit(_ context.Context, _ Event) {}

// SlogEmitter writes events to slog at Info level. Production deployments
// keep this in the fanout next to the SQLite store so events appear in the
// stdout log too.
type SlogEmitter struct {
	Log *slog.Logger
}

// Emit writes the event to the underlying slog.Logger.
func (s SlogEmitter) Emit(_ context.Context, e Event) {
	if s.Log == nil {
		return
	}
	s.Log.Info("audit",
		"type", e.Type,
		"tenant_id", e.TenantID,
		"session_id", e.SessionID,
		"user_id", e.UserID,
		"occurred_at", e.OccurredAt.Format(time.RFC3339Nano),
		"trace_id", e.TraceID,
		"span_id", e.SpanID,
		"payload", e.Payload,
	)
}

// SliceEmitter accumulates events in memory. Tests use it to assert the
// runtime emitted the expected sequence.
type SliceEmitter struct {
	mu     sync.Mutex
	events []Event
}

// Emit appends to the in-memory slice.
func (s *SliceEmitter) Emit(_ context.Context, e Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

// Events returns a copy of the accumulated events in insertion order.
func (s *SliceEmitter) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// Reset drops every accumulated event.
func (s *SliceEmitter) Reset() {
	s.mu.Lock()
	s.events = nil
	s.mu.Unlock()
}
