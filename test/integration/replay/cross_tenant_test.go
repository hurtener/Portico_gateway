// Package replay hosts Phase 11 integration tests for the
// telemetry-replay surface: bundle assembler, importer, audit FTS,
// span store. Full-stack HTTP coverage lives in the smoke script;
// these tests drive the typed seams.
//
// AC #11: tenantA cannot fetch a bundle, export a session, or import
// into tenantB. We test every surface that takes a tenant id.
package replay

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/sessionbundle"
	bundlesqlite "github.com/hurtener/Portico_gateway/internal/sessionbundle/sqlite"
	sqlitestorage "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
	spanstoresqlite "github.com/hurtener/Portico_gateway/internal/telemetry/spanstore/sqlite"
)

func openDB(t *testing.T) *sqlitestorage.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "replay.db") + "?cache=shared"
	db, err := sqlitestorage.Open(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedSession writes a session row + the bound snapshot + a couple
// of audit events so the bundle loader has something to assemble.
// Returns the session id of the seeded row.
func seedSession(t *testing.T, db *sqlitestorage.DB, tenantID, sessionID string, auditStore *audit.Store) {
	t.Helper()
	ctx := context.Background()

	// Insert tenant first — sessions has a FK to tenants.
	if _, err := db.SQL().ExecContext(ctx,
		`INSERT INTO tenants (id, display_name, plan, created_at, updated_at)
		 VALUES (?, ?, 'free', ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		tenantID, tenantID,
		time.Now().UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	// Snapshot first so StampSession can find the tenant.
	_, err := db.SQL().ExecContext(ctx,
		`INSERT INTO catalog_snapshots (id, tenant_id, session_id, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"snap-"+sessionID, tenantID, sessionID,
		`{"servers":[]}`,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}

	if err := db.Snapshots().StampSession(ctx, sessionID, "snap-"+sessionID); err != nil {
		t.Fatalf("stamp session: %v", err)
	}

	for _, ev := range []audit.Event{
		{Type: "tool_call.start", TenantID: tenantID, SessionID: sessionID, Payload: map[string]any{"tool": "x"}},
		{Type: "tool_call.complete", TenantID: tenantID, SessionID: sessionID},
	} {
		if err := auditStore.EmitSync(ctx, ev); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}
}

func TestE2E_TenantIsolation_BundleLoader(t *testing.T) {
	db := openDB(t)
	auditStore := audit.NewStore(db.SQL(), nil)

	seedSession(t, db, "tenant-a", "sess-a-1", auditStore)
	seedSession(t, db, "tenant-b", "sess-b-1", auditStore)

	loader := &sessionbundle.Loader{
		Sessions:  bundlesqlite.NewSessionReader(db.SQL()),
		Snapshots: db.Snapshots(),
		Audit:     auditStore,
		Approvals: db.Approvals(),
		Spans:     spanstoresqlite.New(db.SQL()),
	}

	// Tenant A can load its own session.
	if _, err := loader.Load(context.Background(), "tenant-a", "sess-a-1"); err != nil {
		t.Fatalf("tenant-a own load: %v", err)
	}

	// Tenant A cannot load Tenant B's session.
	if _, err := loader.Load(context.Background(), "tenant-a", "sess-b-1"); err == nil {
		t.Fatal("expected ErrSessionNotFound for cross-tenant load")
	}

	// Symmetric: tenant B cannot load tenant A's session.
	if _, err := loader.Load(context.Background(), "tenant-b", "sess-a-1"); err == nil {
		t.Fatal("expected ErrSessionNotFound for cross-tenant load (symmetric)")
	}
}

func TestE2E_TenantIsolation_AuditSearch(t *testing.T) {
	db := openDB(t)
	auditStore := audit.NewStore(db.SQL(), nil)

	seedSession(t, db, "tenant-a", "sess-a-1", auditStore)
	seedSession(t, db, "tenant-b", "sess-b-1", auditStore)

	// Tenant A search returns only tenant-a events.
	res, err := auditStore.Search(context.Background(), audit.SearchQuery{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, e := range res.Events {
		if e.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak in search: %v", e)
		}
	}
	if len(res.Events) == 0 {
		t.Fatal("tenant-a search returned no events")
	}

	// FTS query also stays within tenant.
	res2, err := auditStore.Search(context.Background(), audit.SearchQuery{
		TenantID: "tenant-a",
		Q:        "tool_call*",
	})
	if err != nil {
		t.Fatalf("fts search: %v", err)
	}
	for _, e := range res2.Events {
		if e.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak in FTS search: %v", e)
		}
	}
}

func TestE2E_TenantIsolation_SpanStore(t *testing.T) {
	db := openDB(t)
	store := spanstoresqlite.New(db.SQL())
	ctx := context.Background()

	if err := store.Put(ctx, []spanstore.Span{
		{
			TenantID:  "tenant-a",
			SessionID: "sess-a-1",
			TraceID:   "trace-shared",
			SpanID:    "span-a-1",
			Name:      "a-only",
			Kind:      spanstore.KindClient,
			StartedAt: time.Now().UTC(),
			EndedAt:   time.Now().UTC().Add(time.Millisecond),
			Status:    spanstore.StatusOK,
		},
		{
			TenantID:  "tenant-b",
			SessionID: "sess-b-1",
			TraceID:   "trace-shared",
			SpanID:    "span-b-1",
			Name:      "b-only",
			Kind:      spanstore.KindClient,
			StartedAt: time.Now().UTC(),
			EndedAt:   time.Now().UTC().Add(time.Millisecond),
			Status:    spanstore.StatusOK,
		},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Tenant A by-trace returns only tenant-a span — even though
	// tenant-b shares the trace id.
	a, err := store.QueryByTrace(ctx, "tenant-a", "trace-shared")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(a) != 1 || a[0].Name != "a-only" {
		t.Errorf("tenant-a query leaked: %+v", a)
	}

	b, err := store.QueryByTrace(ctx, "tenant-b", "trace-shared")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(b) != 1 || b[0].Name != "b-only" {
		t.Errorf("tenant-b query leaked: %+v", b)
	}
}

func TestE2E_TenantIsolation_ImportedBundles(t *testing.T) {
	db := openDB(t)
	auditStore := audit.NewStore(db.SQL(), nil)

	// Seed tenant A and export.
	seedSession(t, db, "tenant-a", "sess-a-1", auditStore)
	loader := &sessionbundle.Loader{
		Sessions:  bundlesqlite.NewSessionReader(db.SQL()),
		Snapshots: db.Snapshots(),
		Audit:     auditStore,
		Approvals: db.Approvals(),
		Spans:     spanstoresqlite.New(db.SQL()),
	}
	bundle, err := loader.Load(context.Background(), "tenant-a", "sess-a-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Tenant-b also needs a tenants row before imported_sessions FK
	// resolves. We make it directly because seedSession is overkill
	// (no session needed on the receiving side).
	if _, err := db.SQL().ExecContext(context.Background(),
		`INSERT INTO tenants (id, display_name, plan, created_at, updated_at)
		 VALUES (?, ?, 'free', ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		"tenant-b", "tenant-b",
		time.Now().UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert tenant-b: %v", err)
	}

	// Import into tenant-b.
	bundleStore := bundlesqlite.New(db.SQL())
	im := &sessionbundle.Importer{Sink: bundleStore}

	var buf bytes.Buffer
	if err := sessionbundle.Export(context.Background(), bundle, &buf, sessionbundle.ExportOptions{}); err != nil {
		t.Fatalf("export: %v", err)
	}
	res, err := im.Import(context.Background(), "tenant-b", &buf)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	// Tenant A must not see the imported bundle in their list.
	listA, err := bundleStore.List(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	for _, row := range listA {
		if row.BundleID == res.BundleID {
			t.Errorf("imported bundle visible to source tenant: %v", row)
		}
	}

	// Tenant B must see it.
	listB, err := bundleStore.List(context.Background(), "tenant-b")
	if err != nil {
		t.Fatalf("list b: %v", err)
	}
	found := false
	for _, row := range listB {
		if row.BundleID == res.BundleID {
			found = true
			if row.SourceTenantID != "tenant-a" {
				t.Errorf("source tenant lost: %q", row.SourceTenantID)
			}
		}
	}
	if !found {
		t.Error("imported bundle not visible to target tenant")
	}

	// Tenant A cannot load the imported bundle by its synthetic id.
	if _, err := bundleStore.LoadImported(context.Background(), "tenant-a", res.SyntheticSessionID); err == nil {
		t.Error("tenant-a should not load tenant-b's imported bundle")
	}
}
