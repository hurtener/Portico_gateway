package ifaces

import (
	"context"
	"time"
)

// AuditEvent is the canonical event shape used across Portico. Phase 5 wires
// the production emitter; Phase 0 ships only the Query path returning empty.
type AuditEvent struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	Type       string         `json:"type"`
	SessionID  string         `json:"session_id,omitempty"`
	UserID     string         `json:"user_id,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
	TraceID    string         `json:"trace_id,omitempty"`
	SpanID     string         `json:"span_id,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

// AuditQuery filters Query results.
type AuditQuery struct {
	TenantID string // required (admin pass-through is checked at handler level)
	Types    []string
	Since    time.Time
	Until    time.Time
	Limit    int
	Cursor   string
}

// AuditStore persists structured audit events.
//
// Append is the write path (Phase 5).
// Query returns events oldest-first (or newest-first if Cursor encoded so) and an opaque next cursor.
type AuditStore interface {
	Append(ctx context.Context, e *AuditEvent) error
	Query(ctx context.Context, q AuditQuery) ([]*AuditEvent, string, error)
}
