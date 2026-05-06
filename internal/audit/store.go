package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Default tuning. These mirror the phase plan: 200ms or 100-event batches
// keep the request path responsive without hammering SQLite.
const (
	defaultBufferSize = 4096
	defaultBatchSize  = 100
	defaultBatchEvery = 200 * time.Millisecond
)

// Store is the SQLite-backed audit sink. Events are queued on a bounded
// channel; a worker batches them into the audit_events table. On overflow
// the oldest queued event is dropped and an `audit.dropped` self-event is
// recorded.
type Store struct {
	db        *sql.DB
	log       *slog.Logger
	redactor  *Redactor
	queue     chan Event
	batchSize int
	batchEvry time.Duration

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	wg        sync.WaitGroup

	droppedMu sync.Mutex
	dropped   int
}

// StoreOption configures a Store.
type StoreOption func(*Store)

// WithRedactor swaps the default redactor (built from NewDefaultRedactor)
// for a custom one — useful in tests that want to assert raw payloads or
// in deployments that need extra rules.
func WithRedactor(r *Redactor) StoreOption {
	return func(s *Store) { s.redactor = r }
}

// WithBufferSize overrides the in-memory queue depth (default 4096).
func WithBufferSize(n int) StoreOption {
	return func(s *Store) {
		if n > 0 {
			s.queue = make(chan Event, n)
		}
	}
}

// WithBatchSize overrides the per-flush event count (default 100).
func WithBatchSize(n int) StoreOption {
	return func(s *Store) {
		if n > 0 {
			s.batchSize = n
		}
	}
}

// WithBatchInterval overrides the flush cadence (default 200ms).
func WithBatchInterval(d time.Duration) StoreOption {
	return func(s *Store) {
		if d > 0 {
			s.batchEvry = d
		}
	}
}

// NewStore constructs a Store backed by db. Call Start before issuing
// Emit so the worker goroutine is running.
func NewStore(db *sql.DB, log *slog.Logger, opts ...StoreOption) *Store {
	if log == nil {
		log = slog.Default()
	}
	s := &Store{
		db:        db,
		log:       log,
		redactor:  NewDefaultRedactor(),
		queue:     make(chan Event, defaultBufferSize),
		batchSize: defaultBatchSize,
		batchEvry: defaultBatchEvery,
		stop:      make(chan struct{}),
	}
	for _, o := range opts {
		o(s)
	}
	if s.redactor == nil {
		s.redactor = NewDefaultRedactor()
	}
	return s
}

// Start kicks off the worker goroutine. Idempotent.
func (s *Store) Start() {
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

// Stop signals the worker to drain and joins it. Idempotent.
func (s *Store) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
		s.wg.Wait()
	})
}

// Emit enqueues an event for async persistence. Drop-oldest on overflow;
// the drop is reflected in periodic `audit.dropped` self-events. Never
// blocks the caller.
func (s *Store) Emit(_ context.Context, e Event) {
	if e.Type == "" || e.TenantID == "" {
		return
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	if s.redactor != nil && e.Payload != nil {
		e.Payload = s.redactor.Redact(e.Payload)
	}
	select {
	case s.queue <- e:
	default:
		// drop-oldest
		select {
		case <-s.queue:
			s.recordDrop()
		default:
		}
		select {
		case s.queue <- e:
		default:
			s.recordDrop()
		}
	}
}

// EmitSync persists the event before returning. Tests use this to avoid
// race-detector noise on assertion paths.
func (s *Store) EmitSync(ctx context.Context, e Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	if s.redactor != nil && e.Payload != nil {
		e.Payload = s.redactor.Redact(e.Payload)
	}
	return s.insertOne(ctx, e)
}

// Query is the filter set callers pass to Store.Query.
type Query struct {
	TenantID string    // required for non-admin callers
	Type     string    // optional exact match
	Since    time.Time // optional lower bound (inclusive)
	Until    time.Time // optional upper bound (exclusive)
	Limit    int       // default 100, capped at 500
	Cursor   string    // opaque, returned by previous Query call
}

// Query reads back persisted events for (tenant, filters). Returns the
// matching slice plus a cursor to pass back for the next page.
func (s *Store) Query(ctx context.Context, q Query) ([]Event, string, error) {
	if q.TenantID == "" {
		return nil, "", fmt.Errorf("audit: query requires tenant id")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	args := []any{q.TenantID}
	where := "WHERE tenant_id = ?"
	if q.Type != "" {
		where += " AND type = ?"
		args = append(args, q.Type)
	}
	if !q.Since.IsZero() {
		where += " AND occurred_at >= ?"
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}
	if !q.Until.IsZero() {
		where += " AND occurred_at < ?"
		args = append(args, q.Until.UTC().Format(time.RFC3339Nano))
	}
	if q.Cursor != "" {
		where += " AND id < ?"
		args = append(args, q.Cursor)
	}
	args = append(args, q.Limit)
	// G202 false positive: `where` is built from a fixed-shape clause
	// list above; only values flow through `args` via placeholders.
	//nolint:gosec // dynamic clause assembly with parameterised values
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, type, session_id, user_id, occurred_at, trace_id, span_id, payload_json
		 FROM audit_events `+where+`
		 ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	out := make([]Event, 0, q.Limit)
	var lastID string
	for rows.Next() {
		var (
			id, ttype, occurred      string
			sessID, userID           sql.NullString
			traceID, spanID, payload sql.NullString
			tenantID                 string
		)
		if err := rows.Scan(&id, &tenantID, &ttype, &sessID, &userID, &occurred, &traceID, &spanID, &payload); err != nil {
			return nil, "", err
		}
		t, _ := time.Parse(time.RFC3339Nano, occurred)
		ev := Event{
			Type:       ttype,
			TenantID:   tenantID,
			SessionID:  sessID.String,
			UserID:     userID.String,
			OccurredAt: t,
			TraceID:    traceID.String,
			SpanID:     spanID.String,
		}
		if payload.Valid && payload.String != "" {
			_ = json.Unmarshal([]byte(payload.String), &ev.Payload)
		}
		out = append(out, ev)
		lastID = id
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) == q.Limit {
		next = lastID
	}
	return out, next, nil
}

func (s *Store) recordDrop() {
	s.droppedMu.Lock()
	s.dropped++
	s.droppedMu.Unlock()
}

func (s *Store) drainDropCount() int {
	s.droppedMu.Lock()
	n := s.dropped
	s.dropped = 0
	s.droppedMu.Unlock()
	return n
}

func (s *Store) run() {
	defer s.wg.Done()
	t := time.NewTicker(s.batchEvry)
	defer t.Stop()

	pending := make([]Event, 0, s.batchSize)
	flush := func() {
		if len(pending) == 0 {
			if n := s.drainDropCount(); n > 0 {
				s.emitDropEvent(n)
			}
			return
		}
		if err := s.insertBatch(context.Background(), pending); err != nil {
			s.log.Warn("audit: batch insert failed", "err", err, "n", len(pending))
		}
		pending = pending[:0]
		if n := s.drainDropCount(); n > 0 {
			s.emitDropEvent(n)
		}
	}

	for {
		select {
		case <-s.stop:
			// Drain remaining queued events before exit.
			for {
				select {
				case ev := <-s.queue:
					pending = append(pending, ev)
					if len(pending) >= s.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case ev := <-s.queue:
			pending = append(pending, ev)
			if len(pending) >= s.batchSize {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

func (s *Store) emitDropEvent(n int) {
	ev := Event{
		Type:       EventAuditDropped,
		TenantID:   "_system",
		OccurredAt: time.Now().UTC(),
		Payload:    map[string]any{"dropped_count": n},
	}
	if err := s.insertOne(context.Background(), ev); err != nil {
		s.log.Warn("audit: failed to record drop event", "err", err)
	}
}

func (s *Store) insertBatch(ctx context.Context, evs []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO audit_events
			(id, tenant_id, type, session_id, user_id, occurred_at, trace_id, span_id, payload_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range evs {
		if _, err := stmt.ExecContext(ctx,
			newAuditID(e.OccurredAt),
			e.TenantID,
			e.Type,
			nullString(e.SessionID),
			nullString(e.UserID),
			e.OccurredAt.UTC().Format(time.RFC3339Nano),
			nullString(e.TraceID),
			nullString(e.SpanID),
			marshalPayload(e.Payload),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) insertOne(ctx context.Context, e Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events
			(id, tenant_id, type, session_id, user_id, occurred_at, trace_id, span_id, payload_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newAuditID(e.OccurredAt),
		e.TenantID,
		e.Type,
		nullString(e.SessionID),
		nullString(e.UserID),
		e.OccurredAt.UTC().Format(time.RFC3339Nano),
		nullString(e.TraceID),
		nullString(e.SpanID),
		marshalPayload(e.Payload),
	)
	return err
}

// newAuditID returns a sortable id keyed on event time. Using ULID gives us
// monotonic ordering across batched inserts (the cursor pagination relies
// on `id < ?` returning chronologically earlier events).
func newAuditID(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), ulid.DefaultEntropy()).String()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func marshalPayload(p map[string]any) string {
	if len(p) == 0 {
		return ""
	}
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(b)
}
