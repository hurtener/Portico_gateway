package sqlite

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	storage "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "spanstore.db") + "?cache=shared"
	db, err := storage.Open(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// We need a tenant row so FK from any later schema attaches; the
	// spanstore itself doesn't FK into tenants, but other migrations
	// expect at least one tenant for sanity tests.
	_, err = db.SQL().Exec(`INSERT INTO tenants(id, display_name, plan) VALUES ('t1', 't1', 'free')`)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return db.SQL()
}

func mkSpan(tenant, session, traceID, spanID, name string, started time.Time) spanstore.Span {
	return spanstore.Span{
		TenantID:  tenant,
		SessionID: session,
		TraceID:   traceID,
		SpanID:    spanID,
		Name:      name,
		Kind:      spanstore.KindInternal,
		Status:    spanstore.StatusOK,
		StartedAt: started,
		EndedAt:   started.Add(50 * time.Millisecond),
		Attrs:     map[string]any{"http.method": "POST", "tool": "github.create_issue"},
	}
}

func TestStore_PutAndQueryBySession(t *testing.T) {
	s := New(newDB(t))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	batch := []spanstore.Span{
		mkSpan("t1", "sess1", "tr1", "sp1", "first", now),
		mkSpan("t1", "sess1", "tr1", "sp2", "second", now.Add(10*time.Millisecond)),
	}
	if err := s.Put(ctx, batch); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.QueryBySession(ctx, "t1", "sess1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(got))
	}
	if got[0].Name != "first" || got[1].Name != "second" {
		t.Errorf("expected ordering by started_at asc, got %q,%q", got[0].Name, got[1].Name)
	}
	// Round-trip attrs.
	if got[0].Attrs["tool"] != "github.create_issue" {
		t.Errorf("expected attrs preserved; got %#v", got[0].Attrs)
	}
}

func TestStore_QueryByTrace_OrdersByStart(t *testing.T) {
	s := New(newDB(t))
	ctx := context.Background()
	now := time.Now().UTC()
	// Inserted out of time order; query must sort.
	if err := s.Put(ctx, []spanstore.Span{
		mkSpan("t1", "", "tr1", "sp_late", "late", now.Add(time.Second)),
		mkSpan("t1", "", "tr1", "sp_early", "early", now),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.QueryByTrace(ctx, "t1", "tr1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 || got[0].Name != "early" || got[1].Name != "late" {
		t.Fatalf("expected early,late; got %#v", got)
	}
}

func TestStore_Purge_RespectsBefore(t *testing.T) {
	s := New(newDB(t))
	ctx := context.Background()
	old := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()
	if err := s.Put(ctx, []spanstore.Span{
		mkSpan("t1", "sess1", "tr1", "old", "old", old),
		mkSpan("t1", "sess1", "tr1", "recent", "recent", recent),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	n, err := s.Purge(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 1 {
		t.Errorf("purge: expected 1 deleted; got %d", n)
	}
	rest, _ := s.QueryBySession(ctx, "t1", "sess1")
	if len(rest) != 1 || rest[0].Name != "recent" {
		t.Errorf("after purge: expected only 'recent'; got %#v", rest)
	}
}

func TestStore_TenantIsolation(t *testing.T) {
	db := newDB(t)
	if _, err := db.Exec(`INSERT INTO tenants(id, display_name, plan) VALUES ('t2', 't2', 'free')`); err != nil {
		t.Fatalf("seed t2: %v", err)
	}
	s := New(db)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Put(ctx, []spanstore.Span{
		mkSpan("t1", "sess", "tr", "spA", "A", now),
		mkSpan("t2", "sess", "tr", "spB", "B", now),
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	t1Spans, _ := s.QueryBySession(ctx, "t1", "sess")
	t2Spans, _ := s.QueryBySession(ctx, "t2", "sess")
	if len(t1Spans) != 1 || t1Spans[0].Name != "A" {
		t.Errorf("t1 isolation: got %#v", t1Spans)
	}
	if len(t2Spans) != 1 || t2Spans[0].Name != "B" {
		t.Errorf("t2 isolation: got %#v", t2Spans)
	}
}
