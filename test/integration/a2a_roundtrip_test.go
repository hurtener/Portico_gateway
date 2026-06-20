package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/examples/servers/mock/a2amock"
	a2adispatch "github.com/hurtener/Portico_gateway/internal/a2a/dispatch"
	a2anb "github.com/hurtener/Portico_gateway/internal/a2a/northbound/http"
	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	a2asb "github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	a2ahttp "github.com/hurtener/Portico_gateway/internal/a2a/southbound/http"
	a2amgr "github.com/hurtener/Portico_gateway/internal/a2a/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// TestE2E_A2A_OutboundRoundtrip exercises the full governed A2A path with every
// real component: northbound transport handler → dispatch (profile enforcement
// + audit) → southbound pool → HTTP client → a real mock A2A peer. It proves an
// inbound message/send naming a registered peer round-trips to that peer and the
// peer's response comes back verbatim (acceptance #1 card identity + #3 dispatch).
func TestE2E_A2A_OutboundRoundtrip(t *testing.T) {
	ctx := context.Background()

	// A real mock A2A peer that echoes the inbound text part.
	peerSrv := httptest.NewServer(a2amock.Handler(a2amock.Options{Name: "mock-peer"}))
	defer peerSrv.Close()

	// SQLite store with the peer registered, pointing at the mock.
	dsn := "file:" + filepath.Join(t.TempDir(), "a2a-e2e.db") + "?cache=shared"
	db, err := sqlite.Open(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := db.A2APeers()
	const peerID = "a2a_e2e1"
	if err := store.PutPeer(ctx, &ifaces.A2APeer{
		TenantID: "t1", ID: peerID, Name: "mock-peer", Endpoint: peerSrv.URL + "/a2a", Enabled: true,
	}); err != nil {
		t.Fatalf("put peer: %v", err)
	}

	// Real pool (no-vault factory) → real dispatch → real northbound handler.
	factory := func(_ context.Context, peer *ifaces.A2APeer) (a2asb.Client, error) {
		return a2ahttp.New(a2ahttp.Config{PeerID: peer.ID, Endpoint: peer.Endpoint}), nil
	}
	pool := a2amgr.NewPool(store, factory, nil)
	disp := a2adispatch.New(store, pool, audit.NopEmitter{}, nil)
	card := func(context.Context, string) a2a.AgentCard {
		return a2a.AgentCard{Name: "Portico", URL: "/a2a", ProtocolVersion: a2a.SpecVersion}
	}
	h := a2anb.NewHandler(disp, card, nil)

	// Inbound message/send naming the registered peer.
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{` +
		`"message":{"role":"user","messageId":"m1","parts":[{"kind":"text","text":"ping"}]},` +
		`"metadata":{"portico_peer":"` + peerID + `"}}}`
	r := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body)).
		WithContext(tenant.With(ctx, tenant.Identity{TenantID: "t1", Scopes: []string{"admin"}}))
	w := httptest.NewRecorder()
	h.RPC(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp a2a.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}
	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("task state = %q, want completed", task.Status.State)
	}
	if len(task.Artifacts) != 1 || len(task.Artifacts[0].Parts) != 1 {
		t.Fatalf("artifacts = %+v", task.Artifacts)
	}
	if got := task.Artifacts[0].Parts[0].Text; got != "ping" {
		t.Errorf("peer echo = %q, want %q", got, "ping")
	}
}
