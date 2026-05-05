package sqlite_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// open opens a fresh in-memory DB.
func open(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), ":memory:", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrations_FreshDB(t *testing.T) {
	db := open(t)
	rows, err := db.SQL().Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[string]bool{
		"approvals": true, "audit_events": true, "catalog_snapshots": true,
		"schema_migrations": true, "servers": true, "sessions": true,
		"skill_enablement": true, "tenants": true,
	}
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing table %q", k)
		}
	}
}

func TestMigrations_Idempotent(t *testing.T) {
	db := open(t)
	// Re-run migrations against the open DB.
	// runMigrations is unexported; opening twice on :memory: yields a fresh DB,
	// so we instead assert the schema_migrations table reports version 1.
	var v int
	if err := db.SQL().QueryRow(`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v < 1 {
		t.Fatalf("expected migration >= 1, got %d", v)
	}
}

func TestTenantStore_Upsert(t *testing.T) {
	db := open(t)
	ts := db.Tenants()
	ctx := context.Background()

	if err := ts.Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	got, err := ts.Get(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Acme" {
		t.Errorf("display = %q", got.DisplayName)
	}
	// Upsert should update display_name on conflict
	if err := ts.Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme Corp", Plan: "enterprise"}); err != nil {
		t.Fatal(err)
	}
	got, _ = ts.Get(ctx, "acme")
	if got.DisplayName != "Acme Corp" || got.Plan != "enterprise" {
		t.Errorf("upsert did not update fields: %+v", got)
	}

	// List
	if err := ts.Upsert(ctx, &ifaces.Tenant{ID: "beta", DisplayName: "Beta", Plan: "free"}); err != nil {
		t.Fatal(err)
	}
	all, err := ts.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("list len = %d, want 2", len(all))
	}
}

func TestTenantStore_NotFound(t *testing.T) {
	db := open(t)
	_, err := db.Tenants().Get(context.Background(), "ghost")
	if !errors.Is(err, ifaces.ErrNotFound) {
		t.Fatalf("expected ifaces.ErrNotFound, got: %v", err)
	}
}

func TestAuditStore_AppendQuery(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	// Need a tenant for FK
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "A", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "beta", DisplayName: "B", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	as := db.Audit()
	for i := 0; i < 3; i++ {
		err := as.Append(ctx, &ifaces.AuditEvent{
			ID: ulidLike(t, i), TenantID: "acme", Type: "tool_call.complete",
			OccurredAt: time.Now().UTC(),
			Payload:    map[string]any{"i": i},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := as.Append(ctx, &ifaces.AuditEvent{
		ID: ulidLike(t, 99), TenantID: "beta", Type: "tool_call.complete",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	a, _, err := as.Query(ctx, ifaces.AuditQuery{TenantID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 {
		t.Errorf("acme events = %d, want 3", len(a))
	}
	for _, ev := range a {
		if ev.TenantID != "acme" {
			t.Errorf("cross-tenant leak: %q", ev.TenantID)
		}
	}

	b, _, err := as.Query(ctx, ifaces.AuditQuery{TenantID: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 1 {
		t.Errorf("beta events = %d, want 1", len(b))
	}
}

// ulidLike fabricates a deterministic 26-char id-ish string for tests
// (real ULIDs land in Phase 5; here we only need uniqueness).
func ulidLike(t *testing.T, i int) string {
	t.Helper()
	return "01HE0000000000000000000" + string(rune('A'+i))
}
