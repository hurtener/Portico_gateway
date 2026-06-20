package manager_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeStore is an in-memory, tenant-scoped A2APeerStore for tests.
type fakeStore struct {
	mu    sync.Mutex
	peers map[string]*ifaces.A2APeer // key: tenant + "/" + id
}

func newFakeStore(peers ...*ifaces.A2APeer) *fakeStore {
	s := &fakeStore{peers: map[string]*ifaces.A2APeer{}}
	for _, p := range peers {
		s.peers[p.TenantID+"/"+p.ID] = p
	}
	return s
}

func (s *fakeStore) PutPeer(_ context.Context, p *ifaces.A2APeer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[p.TenantID+"/"+p.ID] = p
	return nil
}

func (s *fakeStore) GetPeer(_ context.Context, tenantID, id string) (*ifaces.A2APeer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.peers[tenantID+"/"+id]
	if !ok {
		return nil, ifaces.ErrA2APeerNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *fakeStore) ListPeers(_ context.Context, tenantID string) ([]*ifaces.A2APeer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*ifaces.A2APeer
	for _, p := range s.peers {
		if p.TenantID == tenantID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *fakeStore) DeletePeer(_ context.Context, tenantID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := tenantID + "/" + id
	if _, ok := s.peers[k]; !ok {
		return ifaces.ErrA2APeerNotFound
	}
	delete(s.peers, k)
	return nil
}

// stubClient is a southbound.Client that records Close calls.
type stubClient struct {
	peerID string
	closed atomic.Int32
}

func (c *stubClient) FetchAgentCard(context.Context, string) (*a2a.AgentCard, error) { return nil, nil }
func (c *stubClient) SendMessage(context.Context, a2a.MessageSendParams) (json.RawMessage, error) {
	return nil, nil
}
func (c *stubClient) GetTask(context.Context, a2a.TaskQueryParams) (*a2a.Task, error) {
	return nil, nil
}
func (c *stubClient) CancelTask(context.Context, a2a.TaskIDParams) (*a2a.Task, error) {
	return nil, nil
}
func (c *stubClient) Close(context.Context) error {
	c.closed.Add(1)
	return nil
}

// countingFactory returns a fresh stubClient per call and counts builds.
func countingFactory(built *atomic.Int32) manager.ClientFactory {
	return func(_ context.Context, peer *ifaces.A2APeer) (southbound.Client, error) {
		built.Add(1)
		return &stubClient{peerID: peer.ID}, nil
	}
}

func enabledPeer(tenant, id string) *ifaces.A2APeer {
	return &ifaces.A2APeer{TenantID: tenant, ID: id, Name: id, Endpoint: "https://x/a2a", Enabled: true}
}

func TestPool_Acquire_CachesPerPeer(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1"))
	var built atomic.Int32
	p := manager.NewPool(store, countingFactory(&built), nil)
	ctx := context.Background()

	c1, err := p.Acquire(ctx, "t1", "peer-1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	c2, err := p.Acquire(ctx, "t1", "peer-1")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if c1 != c2 {
		t.Error("acquire returned different clients for the same peer (not cached)")
	}
	if got := built.Load(); got != 1 {
		t.Errorf("factory called %d times, want 1 (cached)", got)
	}
}

func TestPool_Acquire_DisabledPeer(t *testing.T) {
	peer := enabledPeer("t1", "peer-1")
	peer.Enabled = false
	store := newFakeStore(peer)
	var built atomic.Int32
	p := manager.NewPool(store, countingFactory(&built), nil)

	_, err := p.Acquire(context.Background(), "t1", "peer-1")
	if !errors.Is(err, manager.ErrPeerDisabled) {
		t.Fatalf("want ErrPeerDisabled, got %v", err)
	}
	if built.Load() != 0 {
		t.Error("factory should not be called for a disabled peer")
	}
}

func TestPool_Acquire_NotFound(t *testing.T) {
	store := newFakeStore()
	var built atomic.Int32
	p := manager.NewPool(store, countingFactory(&built), nil)

	_, err := p.Acquire(context.Background(), "t1", "ghost")
	if !errors.Is(err, ifaces.ErrA2APeerNotFound) {
		t.Fatalf("want ErrA2APeerNotFound, got %v", err)
	}
}

func TestPool_Acquire_TenantIsolation(t *testing.T) {
	// Same peer id in two tenants must yield two distinct clients, never a leak.
	store := newFakeStore(enabledPeer("t1", "peer-1"), enabledPeer("t2", "peer-1"))
	var built atomic.Int32
	p := manager.NewPool(store, countingFactory(&built), nil)
	ctx := context.Background()

	a, err := p.Acquire(ctx, "t1", "peer-1")
	if err != nil {
		t.Fatalf("acquire t1: %v", err)
	}
	b, err := p.Acquire(ctx, "t2", "peer-1")
	if err != nil {
		t.Fatalf("acquire t2: %v", err)
	}
	if a == b {
		t.Error("cross-tenant client leak: same client for t1 and t2")
	}
	if got := built.Load(); got != 2 {
		t.Errorf("factory called %d times, want 2 (one per tenant)", got)
	}
	// t2 must not be able to reach a peer that only exists in t1.
	store2 := newFakeStore(enabledPeer("t1", "only-t1"))
	p2 := manager.NewPool(store2, countingFactory(&built), nil)
	if _, err := p2.Acquire(ctx, "t2", "only-t1"); !errors.Is(err, ifaces.ErrA2APeerNotFound) {
		t.Errorf("t2 reached t1's peer: %v", err)
	}
}

func TestPool_Invalidate_ClosesAndRebuilds(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1"))
	var built atomic.Int32
	p := manager.NewPool(store, countingFactory(&built), nil)
	ctx := context.Background()

	c1, err := p.Acquire(ctx, "t1", "peer-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	p.Invalidate(ctx, "t1", "peer-1")
	if sc := c1.(*stubClient); sc.closed.Load() != 1 {
		t.Errorf("invalidate did not close the client (closed=%d)", sc.closed.Load())
	}
	c2, err := p.Acquire(ctx, "t1", "peer-1")
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if c1 == c2 {
		t.Error("invalidate did not force a rebuild")
	}
	if got := built.Load(); got != 2 {
		t.Errorf("factory called %d times, want 2 (rebuild after invalidate)", got)
	}
	// Invalidating an uncached peer is a no-op (must not panic).
	p.Invalidate(ctx, "t1", "never-cached")
}

func TestPool_CloseAll(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1"), enabledPeer("t1", "peer-2"))
	var built atomic.Int32
	p := manager.NewPool(store, countingFactory(&built), nil)
	ctx := context.Background()

	c1, _ := p.Acquire(ctx, "t1", "peer-1")
	c2, _ := p.Acquire(ctx, "t1", "peer-2")
	if err := p.CloseAll(ctx); err != nil {
		t.Fatalf("close all: %v", err)
	}
	if c1.(*stubClient).closed.Load() != 1 || c2.(*stubClient).closed.Load() != 1 {
		t.Error("CloseAll did not close every cached client")
	}
	// After CloseAll the cache is empty → next Acquire rebuilds.
	if _, err := p.Acquire(ctx, "t1", "peer-1"); err != nil {
		t.Fatalf("acquire after close-all: %v", err)
	}
	if got := built.Load(); got != 3 {
		t.Errorf("factory called %d times, want 3 (2 + 1 rebuild)", got)
	}
}
