package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestA2APeerStore_RoundTrip(t *testing.T) {
	db := open(t)
	store := db.A2APeers()
	ctx := context.Background()

	card := `{"name":"peer-agent","skills":[{"id":"echo"}]}`
	p := &ifaces.A2APeer{
		TenantID:      "tenant-a",
		ID:            "peer-1",
		Name:          "peer-a",
		Endpoint:      "https://peer.example.com/a2a",
		EgressAuthRef: "vault://tenant-a/peer-a-token",
		AgentCardJSON: card,
		Enabled:       true,
	}
	if err := store.PutPeer(ctx, p); err != nil {
		t.Fatalf("put peer: %v", err)
	}
	got, err := store.GetPeer(ctx, "tenant-a", "peer-1")
	if err != nil {
		t.Fatalf("get peer: %v", err)
	}
	if got.Name != "peer-a" || got.Endpoint != p.Endpoint || !got.Enabled {
		t.Errorf("scalar mismatch: %+v", got)
	}
	if got.EgressAuthRef != p.EgressAuthRef {
		t.Errorf("egress_auth_ref mismatch: %q vs %q", got.EgressAuthRef, p.EgressAuthRef)
	}
	if got.AgentCardJSON != card {
		t.Errorf("agent_card_json mismatch: %q", got.AgentCardJSON)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("timestamps not set: %+v", got)
	}
}

func TestA2APeerStore_PreservesCreatedAtOnUpdate(t *testing.T) {
	db := open(t)
	store := db.A2APeers()
	ctx := context.Background()

	p := &ifaces.A2APeer{
		TenantID: "t", ID: "peer-1", Name: "peer-a",
		Endpoint: "https://a.example/a2a", Enabled: true,
	}
	if err := store.PutPeer(ctx, p); err != nil {
		t.Fatalf("put: %v", err)
	}
	first, err := store.GetPeer(ctx, "t", "peer-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	originalCreated := first.CreatedAt

	// Change every field except validations and re-Put. (We can't deterministically
	// assert updated_at advances without a controllable clock — RFC3339 second
	// precision collapses same-second Puts — but the upsert path is the same
	// statement that writes a fresh updated_at value on every conflict.)
	p.Name = "peer-a-renamed"
	p.Endpoint = "https://a.example/v2/a2a"
	p.EgressAuthRef = "vault://t/peer-a-v2"
	p.AgentCardJSON = `{"name":"v2"}`
	p.Enabled = false
	if err := store.PutPeer(ctx, p); err != nil {
		t.Fatalf("second put: %v", err)
	}
	got, err := store.GetPeer(ctx, "t", "peer-1")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.CreatedAt != originalCreated {
		t.Errorf("created_at not preserved: %q -> %q", originalCreated, got.CreatedAt)
	}
	if got.UpdatedAt == "" {
		t.Errorf("updated_at not set after update: %+v", got)
	}
	if got.Name != "peer-a-renamed" || got.Endpoint != "https://a.example/v2/a2a" ||
		got.EgressAuthRef != "vault://t/peer-a-v2" || got.AgentCardJSON != `{"name":"v2"}` ||
		got.Enabled {
		t.Errorf("update did not apply: %+v", got)
	}
}

func TestA2APeerStore_List_TenantIsolated(t *testing.T) {
	db := open(t)
	store := db.A2APeers()
	ctx := context.Background()

	for _, p := range []*ifaces.A2APeer{
		{TenantID: "tenant-a", ID: "peer-1", Name: "alpha-a", Endpoint: "https://aa/1", Enabled: true},
		{TenantID: "tenant-a", ID: "peer-2", Name: "beta-a", Endpoint: "https://aa/2", Enabled: true},
		{TenantID: "tenant-b", ID: "peer-1", Name: "alpha-b", Endpoint: "https://bb/1", Enabled: true},
	} {
		if err := store.PutPeer(ctx, p); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	a, err := store.ListPeers(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list tenant-a: %v", err)
	}
	if len(a) != 2 {
		t.Fatalf("tenant-a: want 2, got %d", len(a))
	}
	if a[0].Name != "alpha-a" || a[1].Name != "beta-a" {
		t.Errorf("ORDER BY name ASC violated: %+v", a)
	}
	for _, p := range a {
		if p.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak: %+v", p)
		}
	}
	b, err := store.ListPeers(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(b) != 1 || b[0].TenantID != "tenant-b" || b[0].Name != "alpha-b" {
		t.Errorf("tenant-b mismatch: %+v", b)
	}
}

func TestA2APeerStore_GetPeer_NotFound(t *testing.T) {
	db := open(t)
	store := db.A2APeers()
	if _, err := store.GetPeer(context.Background(), "tenant-a", "ghost"); !errors.Is(err, ifaces.ErrA2APeerNotFound) {
		t.Fatalf("want ErrA2APeerNotFound, got %v", err)
	}
}

func TestA2APeerStore_DeletePeer(t *testing.T) {
	db := open(t)
	store := db.A2APeers()
	ctx := context.Background()

	p := &ifaces.A2APeer{
		TenantID: "t", ID: "peer-1", Name: "peer-a", Endpoint: "https://a/x", Enabled: true,
	}
	if err := store.PutPeer(ctx, p); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.DeletePeer(ctx, "t", "peer-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetPeer(ctx, "t", "peer-1"); !errors.Is(err, ifaces.ErrA2APeerNotFound) {
		t.Fatalf("want ErrA2APeerNotFound after delete, got %v", err)
	}
	if err := store.DeletePeer(ctx, "t", "peer-1"); !errors.Is(err, ifaces.ErrA2APeerNotFound) {
		t.Fatalf("delete absent: want ErrA2APeerNotFound, got %v", err)
	}
}
