// Package manager owns southbound A2A Client lifecycles. It caches one Client
// per (tenant, peer) so repeated dispatches to the same registered peer reuse a
// single client, and it is the seam where peer specs (from the A2APeerStore)
// and egress credentials are turned into a live client.
//
// Credential resolution is deliberately NOT done here: the Pool takes an
// injected ClientFactory that the binary entry point wires with the vault, so
// the pool stays vault-agnostic and unit-testable (§4.4). Callers depend on the
// southbound.Client interface the Pool returns, never a concrete transport.
package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ClientFactory builds a southbound.Client for a resolved, enabled peer. The
// binary entry point supplies one that resolves the peer's egress auth (e.g. a
// bearer from the vault keyed by peer.EgressAuthRef) and constructs the HTTP
// client. Injected so the Pool never imports the vault and stays testable.
type ClientFactory func(ctx context.Context, peer *ifaces.A2APeer) (southbound.Client, error)

// ErrPeerDisabled is returned by Acquire when the registered peer exists but is
// disabled. Distinct from ifaces.ErrA2APeerNotFound so callers can tell the two
// apart.
var ErrPeerDisabled = errors.New("manager: a2a peer is disabled")

// Pool caches southbound A2A clients per (tenant, peer). It is safe for
// concurrent use.
type Pool struct {
	store   ifaces.A2APeerStore
	factory ClientFactory
	log     *slog.Logger

	mu      sync.Mutex
	clients map[string]southbound.Client
}

// NewPool builds a Pool. store and factory must be non-nil in production; log
// defaults to slog.Default() when nil.
func NewPool(store ifaces.A2APeerStore, factory ClientFactory, log *slog.Logger) *Pool {
	if log == nil {
		log = slog.Default()
	}
	return &Pool{
		store:   store,
		factory: factory,
		log:     log,
		clients: make(map[string]southbound.Client),
	}
}

// key namespaces the cache by tenant first so a peer id can never collide across
// tenants. The NUL separator cannot appear in an id, so the join is unambiguous.
func key(tenantID, peerID string) string { return tenantID + "\x00" + peerID }

// Acquire returns a southbound.Client for the registered peer (tenantID, peerID).
// The peer is looked up tenant-scoped on every call so a disabled/removed peer
// is rejected promptly (ifaces.ErrA2APeerNotFound / ErrPeerDisabled); the built
// client is cached and reused across calls. If the peer's endpoint or egress
// auth changes, call Invalidate so the next Acquire rebuilds it.
func (p *Pool) Acquire(ctx context.Context, tenantID, peerID string) (southbound.Client, error) {
	if p.store == nil || p.factory == nil {
		return nil, errors.New("manager: pool not fully configured")
	}
	if tenantID == "" || peerID == "" {
		return nil, errors.New("manager: acquire requires tenant_id and peer_id")
	}
	peer, err := p.store.GetPeer(ctx, tenantID, peerID)
	if err != nil {
		return nil, err // ifaces.ErrA2APeerNotFound passes through
	}
	if !peer.Enabled {
		return nil, ErrPeerDisabled
	}

	k := key(tenantID, peerID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[k]; ok {
		return c, nil
	}
	c, err := p.factory(ctx, peer)
	if err != nil {
		return nil, fmt.Errorf("manager: build a2a client for peer %s: %w", peerID, err)
	}
	p.clients[k] = c
	p.log.Debug("a2a client created", "tenant_id", tenantID, "peer_id", peerID)
	return c, nil
}

// Invalidate drops and closes the cached client for (tenant, peer), if any.
// Call it when a peer is updated or deleted so the next Acquire rebuilds with
// fresh endpoint/credentials. Safe to call for an uncached peer (no-op).
func (p *Pool) Invalidate(ctx context.Context, tenantID, peerID string) {
	k := key(tenantID, peerID)
	p.mu.Lock()
	c, ok := p.clients[k]
	if ok {
		delete(p.clients, k)
	}
	p.mu.Unlock()
	if ok {
		if err := c.Close(ctx); err != nil {
			p.log.Warn("a2a client close on invalidate failed", "tenant_id", tenantID, "peer_id", peerID, "err", err)
		}
	}
}

// CloseAll closes every cached client and clears the cache. Returns the first
// close error (if any) after attempting to close all. Call on shutdown.
func (p *Pool) CloseAll(ctx context.Context) error {
	p.mu.Lock()
	clients := p.clients
	p.clients = make(map[string]southbound.Client)
	p.mu.Unlock()

	var firstErr error
	for k, c := range clients {
		if err := c.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
			p.log.Warn("a2a client close on shutdown failed", "key", k, "err", err)
		}
	}
	return firstErr
}
