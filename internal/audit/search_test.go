package audit_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hurtener/Portico_gateway/internal/audit"
)

// newSearchDB builds an in-memory SQLite with the audit_events schema
// AND the FTS5 virtual table + triggers from migration 0012. Search()
// can't be exercised without the FTS index in place.
func newSearchDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const schema = `
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
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_events(tenant_id, occurred_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS audit_events_fts USING fts5(
    type, summary, payload_json,
    content='audit_events',
    content_rowid='rowid',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS audit_events_ai AFTER INSERT ON audit_events BEGIN
    INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
    VALUES (new.rowid, new.type, COALESCE(new.summary, ''), COALESCE(new.payload_json, ''));
END;

CREATE TRIGGER IF NOT EXISTS audit_events_ad AFTER DELETE ON audit_events BEGIN
    INSERT INTO audit_events_fts(audit_events_fts, rowid, type, summary, payload_json)
    VALUES ('delete', old.rowid, old.type, COALESCE(old.summary, ''), COALESCE(old.payload_json, ''));
END;

CREATE TRIGGER IF NOT EXISTS audit_events_au AFTER UPDATE ON audit_events BEGIN
    INSERT INTO audit_events_fts(audit_events_fts, rowid, type, summary, payload_json)
    VALUES ('delete', old.rowid, old.type, COALESCE(old.summary, ''), COALESCE(old.payload_json, ''));
    INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
    VALUES (new.rowid, new.type, COALESCE(new.summary, ''), COALESCE(new.payload_json, ''));
END;
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// emit is a thin helper that synchronously inserts an event so the FTS
// index is populated before the test runs Search.
func emit(t *testing.T, s *audit.Store, e audit.Event) {
	t.Helper()
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	if err := s.EmitSync(context.Background(), e); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

func TestSearch_ByText(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	emit(t, s, audit.Event{
		Type:     audit.EventToolCallStart,
		TenantID: "acme",
		Payload:  map[string]any{"tool": "github_search_repositories"},
	})
	emit(t, s, audit.Event{
		Type:     audit.EventToolCallStart,
		TenantID: "acme",
		Payload:  map[string]any{"tool": "slack_post_message"},
	})

	// FTS5 prefix match — `github*` should hit the github row.
	res, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID: "acme",
		Q:        "github*",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 hit; got %d", len(res.Events))
	}
	if got, _ := res.Events[0].Payload["tool"].(string); got != "github_search_repositories" {
		t.Errorf("unexpected hit: %v", res.Events[0].Payload)
	}
}

func TestSearch_EmptyQuery_ListsAll(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	for i := 0; i < 5; i++ {
		emit(t, s, audit.Event{Type: "x", TenantID: "acme"})
	}
	res, err := s.Search(context.Background(), audit.SearchQuery{TenantID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 5 {
		t.Errorf("empty query should list all; got %d / 5", len(res.Events))
	}
}

func TestSearch_ByTimeRange(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	emit(t, s, audit.Event{Type: "x", TenantID: "acme", OccurredAt: base})
	emit(t, s, audit.Event{Type: "x", TenantID: "acme", OccurredAt: base.Add(time.Hour)})
	emit(t, s, audit.Event{Type: "x", TenantID: "acme", OccurredAt: base.Add(2 * time.Hour)})

	res, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID: "acme",
		From:     base.Add(30 * time.Minute),
		To:       base.Add(90 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 row in window; got %d", len(res.Events))
	}
}

func TestSearch_TenantIsolation(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	emit(t, s, audit.Event{
		Type:     audit.EventToolCallStart,
		TenantID: "acme",
		Payload:  map[string]any{"tool": "github_search_repositories"},
	})
	emit(t, s, audit.Event{
		Type:     audit.EventToolCallStart,
		TenantID: "beta",
		Payload:  map[string]any{"tool": "github_search_repositories"},
	})

	res, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID: "acme",
		Q:        "github*",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 hit (tenant scoped); got %d", len(res.Events))
	}
	if res.Events[0].TenantID != "acme" {
		t.Errorf("cross-tenant leak: %v", res.Events[0])
	}
}

func TestSearch_RequiresTenant(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	if _, err := s.Search(context.Background(), audit.SearchQuery{}); err == nil {
		t.Errorf("expected error for missing tenant")
	}
}

func TestSearch_PaginationCursor(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		emit(t, s, audit.Event{
			Type:       "x",
			TenantID:   "acme",
			OccurredAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	page1, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID: "acme",
		Limit:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Events) != 2 || page1.Next == "" {
		t.Fatalf("page1 len=%d next=%q", len(page1.Events), page1.Next)
	}

	page2, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID: "acme",
		Limit:    10,
		Cursor:   page1.Next,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) != 3 {
		t.Errorf("page2 len=%d, want 3", len(page2.Events))
	}
	if page2.Next != "" {
		t.Errorf("page2 should be last (next=%q)", page2.Next)
	}
}

func TestSearch_BySummaryField(t *testing.T) {
	// Verifies the summary column is populated and FTS hits against
	// payload-derived terms work without scanning payload_json.
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	emit(t, s, audit.Event{
		Type:     audit.EventPolicyAllowed,
		TenantID: "acme",
		Payload:  map[string]any{"rule": "github.read.allow", "tool": "github_search_repositories"},
	})
	emit(t, s, audit.Event{
		Type:     audit.EventPolicyDenied,
		TenantID: "acme",
		Payload:  map[string]any{"rule": "linear.write.deny", "tool": "linear_create_issue"},
	})

	// Search by rule name — only the allow row matches.
	res, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID: "acme",
		Q:        "github.read.allow",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 hit; got %d", len(res.Events))
	}

	// Confirm the summary column is actually populated.
	var summary string
	if err := db.QueryRow(`SELECT summary FROM audit_events WHERE type = ? LIMIT 1`,
		audit.EventPolicyAllowed).Scan(&summary); err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if summary == "" {
		t.Errorf("expected summary to be populated, got empty")
	}
}

func TestSearch_FilterBySession(t *testing.T) {
	db := newSearchDB(t)
	s := audit.NewStore(db, discardLogger())
	emit(t, s, audit.Event{Type: "x", TenantID: "acme", SessionID: "sess-A"})
	emit(t, s, audit.Event{Type: "x", TenantID: "acme", SessionID: "sess-B"})
	emit(t, s, audit.Event{Type: "x", TenantID: "acme", SessionID: "sess-A"})

	res, err := s.Search(context.Background(), audit.SearchQuery{
		TenantID:  "acme",
		SessionID: "sess-A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 2 {
		t.Fatalf("expected 2 sess-A events; got %d", len(res.Events))
	}
	for _, e := range res.Events {
		if e.SessionID != "sess-A" {
			t.Errorf("unexpected session: %v", e.SessionID)
		}
	}
}
