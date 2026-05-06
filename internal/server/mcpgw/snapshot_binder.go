package mcpgw

import (
	"context"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

// SnapshotBinder owns the lazy "create the session's snapshot on first
// catalog-touching call" logic. Pure ping sessions never pay the cost.
//
// The binder maintains an in-memory map session_id -> snapshot so the
// dispatcher can fetch the same snapshot for every subsequent call
// without re-reading SQLite. The map is dropped on session close (the
// Session registry's OnClose hook).
type SnapshotBinder struct {
	service *snapshots.Service

	mu       sync.RWMutex
	bindings map[string]*snapshots.Snapshot

	pendingMu sync.Mutex
	pending   map[string]chan struct{}
}

// NewSnapshotBinder wires the binder. service may be nil — the binder is
// then a no-op (catalog calls fall through to live mode automatically).
func NewSnapshotBinder(service *snapshots.Service) *SnapshotBinder {
	return &SnapshotBinder{
		service:  service,
		bindings: make(map[string]*snapshots.Snapshot),
		pending:  make(map[string]chan struct{}),
	}
}

// Get returns the snapshot for the session, creating one if absent.
// Concurrent callers race-safely receive the same snapshot — the second
// caller waits for the first's create to complete instead of duplicating.
func (b *SnapshotBinder) Get(ctx context.Context, sess *Session) (*snapshots.Snapshot, error) {
	if b == nil || b.service == nil || sess == nil {
		return nil, nil
	}
	b.mu.RLock()
	if s, ok := b.bindings[sess.ID]; ok {
		b.mu.RUnlock()
		return s, nil
	}
	b.mu.RUnlock()

	// Coordinate concurrent creators.
	b.pendingMu.Lock()
	if waitCh, ok := b.pending[sess.ID]; ok {
		b.pendingMu.Unlock()
		select {
		case <-waitCh:
			// First creator finished; reread.
			b.mu.RLock()
			s := b.bindings[sess.ID]
			b.mu.RUnlock()
			return s, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	waitCh := make(chan struct{})
	b.pending[sess.ID] = waitCh
	b.pendingMu.Unlock()

	defer func() {
		b.pendingMu.Lock()
		delete(b.pending, sess.ID)
		b.pendingMu.Unlock()
		close(waitCh)
	}()

	snap, err := b.service.Create(ctx, sess.TenantID, sess.ID)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.bindings[sess.ID] = snap
	b.mu.Unlock()
	return snap, nil
}

// Forget drops the binding for a session id. The session registry's
// OnClose hook calls this on close.
func (b *SnapshotBinder) Forget(sessionID string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.bindings, sessionID)
	b.mu.Unlock()
}

// Lookup returns the snapshot for a session without creating one. The
// REST inspector uses this to render the session view without paying the
// snapshot cost when none exists yet.
func (b *SnapshotBinder) Lookup(sessionID string) (*snapshots.Snapshot, bool) {
	if b == nil {
		return nil, false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.bindings[sessionID]
	return s, ok
}
