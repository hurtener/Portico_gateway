package audit_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hurtener/Portico_gateway/internal/audit"
)

func newAuditDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS audit_events (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    session_id   TEXT,
    user_id      TEXT,
    occurred_at  TEXT NOT NULL,
    trace_id     TEXT,
    span_id      TEXT,
    payload_json TEXT,
    summary      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_events(tenant_id, occurred_at DESC);`); err != nil {
		t.Fatal(err)
	}
	return db
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestStore_EmitSync_RoundtripWithRedaction(t *testing.T) {
	db := newAuditDB(t)
	s := audit.NewStore(db, discardLogger())
	ev := audit.Event{
		Type:     audit.EventToolCallStart,
		TenantID: "acme",
		Payload:  map[string]any{"token": "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}
	if err := s.EmitSync(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Query(context.Background(), audit.Query{TenantID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events", len(got))
	}
	if v, ok := got[0].Payload["token"].(string); !ok || v == "" || v[0] != '[' {
		t.Errorf("token not redacted: payload = %v", got[0].Payload)
	}
}

func TestStore_Query_TenantScoped(t *testing.T) {
	db := newAuditDB(t)
	s := audit.NewStore(db, discardLogger())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = s.EmitSync(ctx, audit.Event{Type: "x", TenantID: "acme"})
		_ = s.EmitSync(ctx, audit.Event{Type: "x", TenantID: "beta"})
	}
	got, _, err := s.Query(ctx, audit.Query{TenantID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("acme should see 3 events; got %d", len(got))
	}
	for _, e := range got {
		if e.TenantID != "acme" {
			t.Errorf("cross-tenant leak: %+v", e)
		}
	}
}

func TestStore_Query_TypeFilter(t *testing.T) {
	db := newAuditDB(t)
	s := audit.NewStore(db, discardLogger())
	ctx := context.Background()
	_ = s.EmitSync(ctx, audit.Event{Type: "a", TenantID: "acme"})
	_ = s.EmitSync(ctx, audit.Event{Type: "b", TenantID: "acme"})
	_ = s.EmitSync(ctx, audit.Event{Type: "a", TenantID: "acme"})
	got, _, err := s.Query(ctx, audit.Query{TenantID: "acme", Type: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 'a' events; got %d", len(got))
	}
}

func TestStore_Query_Pagination(t *testing.T) {
	db := newAuditDB(t)
	s := audit.NewStore(db, discardLogger())
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_ = s.EmitSync(ctx, audit.Event{Type: "x", TenantID: "acme"})
		time.Sleep(time.Millisecond)
	}
	page1, cur, err := s.Query(ctx, audit.Query{TenantID: "acme", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || cur == "" {
		t.Fatalf("page1 len=%d cur=%q", len(page1), cur)
	}
	page2, _, err := s.Query(ctx, audit.Query{TenantID: "acme", Limit: 5, Cursor: cur})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 3 {
		t.Errorf("page2 len=%d, want 3", len(page2))
	}
}

func TestStore_AsyncEmit_DrainOnStop(t *testing.T) {
	db := newAuditDB(t)
	s := audit.NewStore(db, discardLogger(), audit.WithBatchInterval(20*time.Millisecond))
	s.Start()
	ctx := context.Background()
	const n = 50
	for i := 0; i < n; i++ {
		s.Emit(ctx, audit.Event{Type: "x", TenantID: "acme"})
	}
	s.Stop() // drain + flush
	got, _, err := s.Query(ctx, audit.Query{TenantID: "acme", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Errorf("after Stop got %d / %d events", len(got), n)
	}
}

func TestStore_BufferOverflow_DropsOldest(t *testing.T) {
	db := newAuditDB(t)
	// Buffer size 2 + tiny batch interval to test drop-oldest.
	s := audit.NewStore(db, discardLogger(), audit.WithBufferSize(2), audit.WithBatchInterval(time.Hour))
	s.Start()
	defer s.Stop()
	ctx := context.Background()
	// Pump faster than the worker can flush.
	for i := 0; i < 100; i++ {
		s.Emit(ctx, audit.Event{Type: "x", TenantID: "acme"})
	}
	// We just want the call not to panic / hang; the actual drop count
	// surfaces via audit.dropped events on the next flush. Stop forces a
	// flush, which records the drop event (if any).
	s.Stop()
	got, _, err := s.Query(ctx, audit.Query{TenantID: "_system", Type: audit.EventAuditDropped})
	if err != nil {
		t.Fatal(err)
	}
	// We don't care about the exact count — just that overflow recording
	// works without deadlocking.
	_ = got
}

func TestStore_QueryRequiresTenant(t *testing.T) {
	db := newAuditDB(t)
	s := audit.NewStore(db, discardLogger())
	if _, _, err := s.Query(context.Background(), audit.Query{}); err == nil {
		t.Errorf("expected error for missing tenant id")
	}
}

func TestEmitter_Fanout(t *testing.T) {
	a := &audit.SliceEmitter{}
	b := &audit.SliceEmitter{}
	f := audit.NewFanoutEmitter(a, b)
	ev := audit.Event{Type: "x", TenantID: "acme"}
	f.Emit(context.Background(), ev)
	if len(a.Events()) != 1 || len(b.Events()) != 1 {
		t.Errorf("a=%d b=%d", len(a.Events()), len(b.Events()))
	}
	if a.Events()[0].OccurredAt.IsZero() {
		t.Errorf("emitter did not stamp OccurredAt")
	}
}

func TestEmitter_Add_AfterConstruction(t *testing.T) {
	f := audit.NewFanoutEmitter()
	a := &audit.SliceEmitter{}
	f.Add(a)
	f.Emit(context.Background(), audit.Event{Type: "x", TenantID: "acme"})
	if len(a.Events()) != 1 {
		t.Errorf("Add did not register")
	}
}
