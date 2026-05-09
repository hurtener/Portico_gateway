// Package spanstore is Portico's self-contained span persistence layer.
// The OTel exporter tees finished spans into a Store *in addition to*
// the configured external collector, so the session inspector can render
// a full trace waterfall without depending on an external trace backend.
//
// Phase 11. The interface lives here; SQLite-backed implementation in
// spanstore/sqlite/.
//
// All methods are tenant-scoped — the Phase 0 multi-tenancy invariant
// (every store method takes tenantID, every WHERE filters on it) carries
// through.
package spanstore

import (
	"context"
	"time"
)

// SpanKind mirrors OTel's SpanKind enum, normalised to the canonical
// lowercase strings stored in the `spans.kind` column.
const (
	KindInternal = "internal"
	KindServer   = "server"
	KindClient   = "client"
	KindProducer = "producer"
	KindConsumer = "consumer"
)

// SpanStatus mirrors OTel's status enum.
const (
	StatusUnset = "unset"
	StatusOK    = "ok"
	StatusError = "error"
)

// SpanEvent is a single time-stamped event recorded against a span.
// We cap event count + attribute size at the writer side to keep the
// store cheap; consumers should not assume the full OTel event surface
// is preserved.
type SpanEvent struct {
	Name      string         `json:"name"`
	Timestamp time.Time      `json:"timestamp"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// Span is the persistence-layer projection of an OTel span. Every
// field is required except SessionID, ParentID, StatusMsg, and Events.
type Span struct {
	TenantID  string
	SessionID string // empty when the span is not associated with a Portico session
	TraceID   string
	SpanID    string
	ParentID  string
	Name      string
	Kind      string
	StartedAt time.Time
	EndedAt   time.Time
	Status    string
	StatusMsg string
	Attrs     map[string]any
	Events    []SpanEvent
}

// Store is the persistence seam. Concrete drivers live under
// `spanstore/<driver>/` and self-register at init time so callers
// depend only on this package.
type Store interface {
	// Put writes a batch of spans. Idempotent: re-putting a span with
	// the same (tenant_id, trace_id, span_id) overwrites the prior row.
	Put(ctx context.Context, batch []Span) error

	// QueryBySession returns every span the store has for the given
	// (tenant, session), ordered by started_at ASC. Returns an empty
	// slice (no error) when no spans match.
	QueryBySession(ctx context.Context, tenantID, sessionID string) ([]Span, error)

	// QueryByTrace returns every span in the given trace for the tenant,
	// ordered by started_at ASC.
	QueryByTrace(ctx context.Context, tenantID, traceID string) ([]Span, error)

	// Purge removes spans that ended before `before`. Returns the number
	// of rows removed. Used by the per-tenant retention worker.
	Purge(ctx context.Context, before time.Time) (int64, error)
}
