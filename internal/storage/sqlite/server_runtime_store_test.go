package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestServerRuntimeStore_RoundTrip(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	store := db.ServerRuntime()

	r := &ifaces.ServerRuntimeRecord{
		TenantID: "acme", ServerID: "github",
		EnvOverrides: []byte(`{"DEBUG":"1"}`),
		Enabled:      true,
	}
	if err := store.Upsert(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "acme", "github")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || string(got.EnvOverrides) != `{"DEBUG":"1"}` {
		t.Errorf("round-trip: %+v", got)
	}

	// RecordRestart updates last_restart fields.
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := store.RecordRestart(ctx, "acme", "github", "config-changed", at); err != nil {
		t.Fatalf("record restart: %v", err)
	}
	got, _ = store.Get(ctx, "acme", "github")
	if got.LastRestartReason != "config-changed" || got.LastRestartAt.IsZero() {
		t.Errorf("restart bookkeeping missing: %+v", got)
	}

	// Delete
	if err := store.Delete(ctx, "acme", "github"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "acme", "github"); !errors.Is(err, ifaces.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestServerRuntimeStore_RecordRestartCreatesRow(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	if err := db.Tenants().Upsert(ctx, &ifaces.Tenant{ID: "acme", DisplayName: "Acme", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
	store := db.ServerRuntime()
	if err := store.RecordRestart(ctx, "acme", "fresh", "first", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "acme", "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastRestartReason != "first" {
		t.Errorf("got %+v", got)
	}
}
