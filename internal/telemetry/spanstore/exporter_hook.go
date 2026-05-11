// exporter_hook.go ties the OTel exporter to the local spanstore.
//
// The OTel exporter must NEVER block on the spanstore writer — a slow
// disk should not back-pressure the trace hot path. We use a bounded
// buffered channel + a single drain goroutine; on overflow we drop the
// oldest queued span and emit a single audit-style log line per drop
// window. Drops are visible (operator can see them) but the call path
// is never stalled.

package spanstore

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SessionAttrKey + TenantAttrKey are the canonical attribute keys
// Portico's transports stamp on every session-bound span so the
// spanstore can index them. The Phase 1 northbound transport sets
// `tenant_id` and `session_id`; Phase 6 instrumentation does the same.
const (
	SessionAttrKey = "session_id"
	TenantAttrKey  = "tenant_id"
)

// HookOptions tunes the tee. Defaults are conservative for a single
// Portico instance.
type HookOptions struct {
	// BufferSize bounds the in-memory queue. When full, oldest-drops.
	// Default: 1024.
	BufferSize int
	// FlushInterval bounds how long a write batch sits in the queue
	// before being flushed. Default: 1s.
	FlushInterval time.Duration
	// MaxBatch bounds how many spans go into one Put call.
	// Default: 128.
	MaxBatch int
	// Logger receives the periodic drop reports + flush errors.
	Logger *slog.Logger
}

func (o *HookOptions) defaults() {
	if o.BufferSize <= 0 {
		o.BufferSize = 1024
	}
	if o.FlushInterval <= 0 {
		o.FlushInterval = time.Second
	}
	if o.MaxBatch <= 0 {
		o.MaxBatch = 128
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// Tee wraps an upstream sdktrace.SpanExporter so that every span it
// receives is ALSO copied into the local Store (best-effort, never
// blocking). The returned SpanExporter forwards all method calls to
// the upstream exporter unchanged; the upstream is the source of
// truth for OTel collector latency / errors.
//
// Closes the queue + drains pending spans on Shutdown.
func Tee(upstream sdktrace.SpanExporter, store Store, opt HookOptions) sdktrace.SpanExporter {
	opt.defaults()
	t := &teeExporter{
		upstream: upstream,
		store:    store,
		queue:    make(chan Span, opt.BufferSize),
		opt:      opt,
		done:     make(chan struct{}),
	}
	t.wg.Add(1)
	go t.drain()
	return t
}

type teeExporter struct {
	upstream sdktrace.SpanExporter
	store    Store
	queue    chan Span
	opt      HookOptions
	done     chan struct{}
	wg       sync.WaitGroup
	dropMu   sync.Mutex
	dropped  int64 // since last log
	lastLog  time.Time
}

// ExportSpans is the OTel hook. We forward to upstream first (so any
// OTel-side error surfaces unchanged) and then enqueue our copies; the
// drain goroutine handles the persistence layer.
func (t *teeExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := t.upstream.ExportSpans(ctx, spans)
	for _, ros := range spans {
		t.enqueue(spanFromReadOnly(ros))
	}
	return err
}

// Shutdown flushes pending spans then shuts down the upstream.
func (t *teeExporter) Shutdown(ctx context.Context) error {
	close(t.done)
	t.wg.Wait()
	return t.upstream.Shutdown(ctx)
}

func (t *teeExporter) enqueue(sp Span) {
	for {
		select {
		case t.queue <- sp:
			return
		default:
			// Queue full: drop oldest by reading one off, then retry.
			select {
			case <-t.queue:
				t.recordDrop()
			default:
				// Queue drained between our checks — just retry.
			}
		}
	}
}

func (t *teeExporter) recordDrop() {
	t.dropMu.Lock()
	defer t.dropMu.Unlock()
	t.dropped++
	if time.Since(t.lastLog) > 10*time.Second {
		t.opt.Logger.Warn("spanstore: dropped spans (overflow)",
			"count", t.dropped,
			"buffer_size", t.opt.BufferSize,
		)
		t.dropped = 0
		t.lastLog = time.Now()
	}
}

func (t *teeExporter) drain() {
	defer t.wg.Done()
	tick := time.NewTicker(t.opt.FlushInterval)
	defer tick.Stop()
	batch := make([]Span, 0, t.opt.MaxBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Detached background ctx: the drain goroutine lives for the
		// process lifetime, not any request. We bound writes to 5s so a
		// stuck SQLite doesn't pile up the queue.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck // intentional: drain is detached
		if err := t.store.Put(ctx, batch); err != nil {
			t.opt.Logger.Warn("spanstore: flush failed", "err", err, "spans", len(batch))
		}
		cancel()
		batch = batch[:0]
	}
	for {
		select {
		case sp, ok := <-t.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, sp)
			if len(batch) >= t.opt.MaxBatch {
				flush()
			}
		case <-tick.C:
			flush()
		case <-t.done:
			// Drain whatever's queued so we don't lose spans on shutdown.
			for {
				select {
				case sp := <-t.queue:
					batch = append(batch, sp)
				default:
					flush()
					return
				}
			}
		}
	}
}

// spanFromReadOnly extracts the persistence-layer projection from an
// OTel ReadOnlySpan. Tenant + session IDs come from the canonical
// attribute keys our transports stamp.
func spanFromReadOnly(ros sdktrace.ReadOnlySpan) Span {
	sp := Span{
		Name:      ros.Name(),
		Kind:      kindString(ros.SpanKind().String()),
		StartedAt: ros.StartTime(),
		EndedAt:   ros.EndTime(),
		Status:    statusString(ros.Status().Code.String()),
		StatusMsg: ros.Status().Description,
	}
	sc := ros.SpanContext()
	if sc.HasTraceID() {
		sp.TraceID = sc.TraceID().String()
	}
	if sc.HasSpanID() {
		sp.SpanID = sc.SpanID().String()
	}
	parent := ros.Parent()
	if parent.HasSpanID() {
		sp.ParentID = parent.SpanID().String()
	}
	attrs := map[string]any{}
	for _, kv := range ros.Attributes() {
		switch string(kv.Key) {
		case TenantAttrKey:
			sp.TenantID = stringValue(kv.Value)
		case SessionAttrKey:
			sp.SessionID = stringValue(kv.Value)
		}
		attrs[string(kv.Key)] = anyValue(kv.Value)
	}
	sp.Attrs = attrs
	for _, ev := range ros.Events() {
		evAttrs := map[string]any{}
		for _, kv := range ev.Attributes {
			evAttrs[string(kv.Key)] = anyValue(kv.Value)
		}
		sp.Events = append(sp.Events, SpanEvent{
			Name:      ev.Name,
			Timestamp: ev.Time,
			Attrs:     evAttrs,
		})
	}
	return sp
}

func kindString(s string) string {
	// OTel returns "SpanKindInternal" / "SpanKindServer" / etc. Strip
	// the prefix and lowercase. Defensive against future spelling.
	s = strings.TrimPrefix(s, "SpanKind")
	switch strings.ToLower(s) {
	case "server":
		return KindServer
	case "client":
		return KindClient
	case "producer":
		return KindProducer
	case "consumer":
		return KindConsumer
	default:
		return KindInternal
	}
}

func statusString(s string) string {
	switch strings.ToLower(s) {
	case "ok":
		return StatusOK
	case "error":
		return StatusError
	default:
		return StatusUnset
	}
}

func stringValue(v attribute.Value) string {
	if v.Type() == attribute.STRING {
		return v.AsString()
	}
	// Fall through: best-effort string representation for non-string
	// types. Tenant/session IDs are always strings on the writer side.
	return v.Emit()
}

func anyValue(v attribute.Value) any {
	switch v.Type() {
	case attribute.STRING:
		return v.AsString()
	case attribute.BOOL:
		return v.AsBool()
	case attribute.INT64:
		return v.AsInt64()
	case attribute.FLOAT64:
		return v.AsFloat64()
	case attribute.STRINGSLICE:
		return v.AsStringSlice()
	case attribute.INT64SLICE:
		return v.AsInt64Slice()
	case attribute.FLOAT64SLICE:
		return v.AsFloat64Slice()
	case attribute.BOOLSLICE:
		return v.AsBoolSlice()
	case attribute.INVALID:
		return nil
	default:
		return v.Emit()
	}
}

// Used by tests + the metrics surface.
var _ = strconv.Itoa
