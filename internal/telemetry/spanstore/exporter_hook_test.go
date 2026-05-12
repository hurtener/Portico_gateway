package spanstore_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
	spanstoresqlite "github.com/hurtener/Portico_gateway/internal/telemetry/spanstore/sqlite"
)

// captureExporter is the upstream we tee from. It just records every
// span the SDK pushes through it so the test can assert "the OTel
// path counted N, the spanstore counted N" without floating point.
type captureExporter struct {
	mu    sync.Mutex
	count int
}

func (c *captureExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	c.mu.Lock()
	c.count += len(spans)
	c.mu.Unlock()
	return nil
}
func (c *captureExporter) Shutdown(_ context.Context) error { return nil }

func newSpanDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "spans.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS spans (
    tenant_id    TEXT NOT NULL,
    session_id   TEXT,
    trace_id     TEXT NOT NULL,
    span_id      TEXT NOT NULL,
    parent_id    TEXT,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    ended_at     TEXT NOT NULL,
    status       TEXT NOT NULL,
    status_msg   TEXT NOT NULL DEFAULT '',
    attrs_json   TEXT NOT NULL DEFAULT '{}',
    events_json  TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (tenant_id, trace_id, span_id)
);
CREATE INDEX IF NOT EXISTS idx_spans_session ON spans(tenant_id, session_id, started_at);`); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestExporterHook_Tees_NoSpanLost is AC #1: every span the OTel
// SDK pushes through the configured exporter also lands in the
// local spanstore. We instantiate a real sdktrace.TracerProvider,
// emit spans with the canonical session/tenant attrs, and assert
// the counts match.
func TestExporterHook_Tees_NoSpanLost(t *testing.T) {
	upstream := &captureExporter{}
	store := spanstoresqlite.New(newSpanDB(t))

	teed := spanstore.Tee(upstream, store, spanstore.HookOptions{
		BufferSize:    64,
		FlushInterval: 20 * time.Millisecond,
		MaxBatch:      8,
	})

	tp := sdktrace.NewTracerProvider(
		// SimpleSpanProcessor is synchronous — every End() pushes
		// straight to the exporter, which is what we want for a
		// counting test.
		sdktrace.WithSyncer(teed),
	)
	defer tp.Shutdown(context.Background()) //nolint:errcheck

	tracer := tp.Tracer("phase-11/test")
	ctx := context.Background()
	const total = 25
	for i := 0; i < total; i++ {
		_, span := tracer.Start(ctx, "phase-11.spanstore.tee")
		span.SetAttributes(
			attribute.String(spanstore.TenantAttrKey, "tenant-x"),
			attribute.String(spanstore.SessionAttrKey, "sess-tee-1"),
		)
		span.End()
	}

	// Force the tee's drain goroutine to flush by shutting down the
	// tracer provider — that fans out a Shutdown to the wrapped
	// exporter, which closes the queue + drains pending spans.
	if err := tp.Shutdown(ctx); err != nil {
		t.Fatalf("tp.Shutdown: %v", err)
	}

	// Upstream count.
	upstream.mu.Lock()
	upstreamCount := upstream.count
	upstream.mu.Unlock()
	if upstreamCount != total {
		t.Errorf("upstream count = %d, want %d", upstreamCount, total)
	}

	// Spanstore count.
	got, err := store.QueryBySession(ctx, "tenant-x", "sess-tee-1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != total {
		t.Errorf("spanstore count = %d, want %d (lost spans)", len(got), total)
	}
	// Quick sanity: every recorded span carries the tenant attr.
	for _, s := range got {
		if s.TenantID != "tenant-x" || s.SessionID != "sess-tee-1" {
			t.Errorf("unexpected ids on stored span: %+v", s)
		}
	}
}

// TestExporterHook_DoesNotBlockUpstream — when the tee's queue is
// full the OTel hot path must NOT stall. We use a tiny buffer + a
// very long flush interval so the drain goroutine doesn't naturally
// keep up; the export call should still return promptly with the
// upstream's result.
func TestExporterHook_DoesNotBlockUpstream(t *testing.T) {
	upstream := &captureExporter{}
	store := spanstoresqlite.New(newSpanDB(t))

	teed := spanstore.Tee(upstream, store, spanstore.HookOptions{
		BufferSize:    2, // intentionally small
		FlushInterval: time.Hour,
		MaxBatch:      4,
	})

	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(teed))
	tracer := tp.Tracer("phase-11/test/overflow")
	ctx := context.Background()

	deadline := time.Now().Add(2 * time.Second)
	const total = 100
	for i := 0; i < total; i++ {
		_, span := tracer.Start(ctx, "overflow")
		span.SetAttributes(attribute.String(spanstore.TenantAttrKey, "tenant-x"))
		span.End()
		if time.Now().After(deadline) {
			t.Fatalf("export hot path stalled around %d/%d", i, total)
		}
	}

	_ = tp.Shutdown(ctx)

	// Upstream MUST have seen all of them — the tee never drops on
	// the OTel path.
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	if upstream.count != total {
		t.Errorf("upstream lost spans: got %d, want %d", upstream.count, total)
	}
}

// guard against an unused dependency the test file would otherwise
// pull in only via type assertions.
var _ trace.Tracer = (sdktrace.NewTracerProvider()).Tracer("noop")
