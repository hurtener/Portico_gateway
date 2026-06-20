package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hurtener/Portico_gateway/examples/servers/mock/a2amock"
	"github.com/hurtener/Portico_gateway/internal/a2a/ingest"
	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	a2ahttp "github.com/hurtener/Portico_gateway/internal/a2a/southbound/http"
	a2amgr "github.com/hurtener/Portico_gateway/internal/a2a/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func TestCardURL(t *testing.T) {
	cases := map[string]string{
		"https://host/a2a":          "https://host/.well-known/agent.json",
		"https://host:8090/rpc?x=1": "https://host:8090/.well-known/agent.json",
		"http://127.0.0.1:9/a2a":    "http://127.0.0.1:9/.well-known/agent.json",
		"not a url":                 "not a url/.well-known/agent.json",
	}
	for in, want := range cases {
		if got := ingest.CardURL(in); got != want {
			t.Errorf("CardURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRefreshCard_FetchesAndPersists(t *testing.T) {
	ctx := context.Background()
	// Real mock peer serving its card at /.well-known/agent.json.
	peerSrv := httptest.NewServer(a2amock.Handler(a2amock.Options{Name: "mock-peer"}))
	defer peerSrv.Close()

	dsn := "file:" + filepath.Join(t.TempDir(), "ingest.db") + "?cache=shared"
	db, err := sqlite.Open(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := db.A2APeers()
	if err := store.PutPeer(ctx, &ifaces.A2APeer{
		TenantID: "t1", ID: "p1", Name: "mock-peer", Endpoint: peerSrv.URL + "/a2a", Enabled: true,
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	factory := func(_ context.Context, peer *ifaces.A2APeer) (southbound.Client, error) {
		return a2ahttp.New(a2ahttp.Config{PeerID: peer.ID, Endpoint: peer.Endpoint}), nil
	}
	pool := a2amgr.NewPool(store, factory, nil)
	r := ingest.NewRefresher(store, pool, nil)

	updated, err := r.RefreshCard(ctx, "t1", "p1")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if updated.AgentCardJSON == "" {
		t.Fatal("agent_card_json not persisted")
	}
	var card a2a.AgentCard
	if err := json.Unmarshal([]byte(updated.AgentCardJSON), &card); err != nil {
		t.Fatalf("decode persisted card: %v", err)
	}
	if card.Name != "mock-peer" || len(card.Skills) == 0 {
		t.Errorf("card = %+v", card)
	}
}

func TestRefreshCard_FetchFailure(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "ingest2.db") + "?cache=shared"
	db, err := sqlite.Open(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := db.A2APeers()
	_ = store.PutPeer(ctx, &ifaces.A2APeer{TenantID: "t1", ID: "p1", Name: "x", Endpoint: "http://127.0.0.1:1/a2a", Enabled: true})

	factory := func(_ context.Context, peer *ifaces.A2APeer) (southbound.Client, error) {
		return a2ahttp.New(a2ahttp.Config{PeerID: peer.ID, Endpoint: peer.Endpoint}), nil
	}
	r := ingest.NewRefresher(store, a2amgr.NewPool(store, factory, nil), nil)
	_, err = r.RefreshCard(ctx, "t1", "p1")
	if !errors.Is(err, ingest.ErrCardFetch) {
		t.Fatalf("want ErrCardFetch for unreachable peer, got %v", err)
	}
}

func TestRefreshCard_PeerNotFound(t *testing.T) {
	ctx := context.Background()
	dsn := "file:" + filepath.Join(t.TempDir(), "ingest3.db") + "?cache=shared"
	db, _ := sqlite.Open(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = db.Close() })
	store := db.A2APeers()
	factory := func(_ context.Context, peer *ifaces.A2APeer) (southbound.Client, error) {
		return a2ahttp.New(a2ahttp.Config{PeerID: peer.ID, Endpoint: peer.Endpoint}), nil
	}
	r := ingest.NewRefresher(store, a2amgr.NewPool(store, factory, nil), nil)
	if _, err := r.RefreshCard(ctx, "t1", "ghost"); !errors.Is(err, ifaces.ErrA2APeerNotFound) {
		t.Fatalf("want ErrA2APeerNotFound, got %v", err)
	}
}
