package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestEntityActivityStore_AppendList(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	store := db.EntityActivity()
	for i := 0; i < 3; i++ {
		if err := store.Append(ctx, &ifaces.EntityActivityRecord{
			TenantID: "acme", EntityKind: "server", EntityID: "github",
			EventID: "evt-" + string(rune('a'+i)), OccurredAt: time.Now().Add(time.Duration(i) * time.Second),
			ActorUserID: "ops",
			Summary:     "server.updated",
			DiffJSON:    []byte(`{"field":"x"}`),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	// Append for a different entity.
	if err := store.Append(ctx, &ifaces.EntityActivityRecord{
		TenantID: "acme", EntityKind: "server", EntityID: "other",
		EventID:  "evt-other",
		Summary:  "server.created",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := store.List(ctx, "acme", "server", "github", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows for github, got %d", len(rows))
	}
	// Ordered by occurred_at DESC: latest first.
	if rows[0].EventID != "evt-c" {
		t.Errorf("expected evt-c first, got %s", rows[0].EventID)
	}
}

func TestEntityActivityStore_TenantIsolation(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	for _, id := range []string{"acme", "beta"} {
		if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: id, DisplayName: id, Plan: "pro"}); err != nil {
			t.Fatal(err)
		}
	}
	store := db.EntityActivity()
	if err := store.Append(ctx, &ifaces.EntityActivityRecord{
		TenantID: "acme", EntityKind: "server", EntityID: "x",
		EventID: "1", Summary: "a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, &ifaces.EntityActivityRecord{
		TenantID: "beta", EntityKind: "server", EntityID: "x",
		EventID: "2", Summary: "b",
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := store.List(ctx, "acme", "server", "x", 100)
	b, _ := store.List(ctx, "beta", "server", "x", 100)
	if len(a) != 1 || a[0].Summary != "a" {
		t.Errorf("acme leaked: %+v", a)
	}
	if len(b) != 1 || b[0].Summary != "b" {
		t.Errorf("beta leaked: %+v", b)
	}
}
