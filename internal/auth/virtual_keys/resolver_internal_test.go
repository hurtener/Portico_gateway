package virtualkeys

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// errStore is a minimal GovernanceStore whose LookupVirtualKeyByID fails. The
// embedded nil interface satisfies the type; only the one method is overridden
// (the resolver calls nothing else).
type errStore struct {
	ifaces.GovernanceStore
	err error
}

func (e errStore) LookupVirtualKeyByID(_ context.Context, _ string) (*ifaces.VirtualKey, error) {
	return nil, e.err
}

// TestResolve_FailsClosedOnStoreError: a genuine store error must NOT
// authenticate — the resolver propagates the error (so the middleware fails
// closed), never returning a *Resolved.
func TestResolve_FailsClosedOnStoreError(t *testing.T) {
	boom := errors.New("db down")
	r := NewResolver(errStore{err: boom}, 0)
	token := ComposeToken("vk_abcdef0123456789abcdef01", "secret")
	got, err := r.Resolve(context.Background(), token)
	if got != nil {
		t.Fatal("must not authenticate on store error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("want propagated store error, got %v", err)
	}
}

// TestResolve_NotFoundIsUnknown maps the storage not-found to the ambiguous
// ErrUnknown (no enumeration signal).
func TestResolve_NotFoundIsUnknown(t *testing.T) {
	r := NewResolver(errStore{err: ifaces.ErrGovernanceNotFound}, 0)
	token := ComposeToken("vk_abcdef0123456789abcdef01", "secret")
	if _, err := r.Resolve(context.Background(), token); !errors.Is(err, ErrUnknown) {
		t.Fatalf("want ErrUnknown, got %v", err)
	}
}

// TestEvictOneLocked covers cache housekeeping: an expired entry is preferred
// for eviction; otherwise an arbitrary entry is dropped.
func TestEvictOneLocked(t *testing.T) {
	r := NewResolver(errStore{}, time.Minute)
	// Seed an expired + a live entry.
	r.cache["expired"] = &cacheEntry{vkID: "vk_old", expiresAt: time.Now().Add(-time.Hour)}
	r.cache["live"] = &cacheEntry{vkID: "vk_new", expiresAt: time.Now().Add(time.Hour)}
	r.evictOneLocked()
	if _, ok := r.cache["expired"]; ok {
		t.Fatal("evictOneLocked should drop the expired entry first")
	}
	if _, ok := r.cache["live"]; !ok {
		t.Fatal("live entry should survive when an expired one exists")
	}
	// With only live entries, eviction still drops one (bounding the cache).
	r.cache["live2"] = &cacheEntry{vkID: "vk_2", expiresAt: time.Now().Add(time.Hour)}
	r.evictOneLocked()
	if len(r.cache) != 1 {
		t.Fatalf("evictOneLocked should drop one live entry, len=%d", len(r.cache))
	}
}

// TestInvalidateVK_WhiteBox confirms only the matching vkID entries are dropped.
func TestInvalidateVK_WhiteBox(t *testing.T) {
	r := NewResolver(errStore{}, time.Minute)
	r.cache["a"] = &cacheEntry{vkID: "vk_keep", expiresAt: time.Now().Add(time.Hour)}
	r.cache["b"] = &cacheEntry{vkID: "vk_drop", expiresAt: time.Now().Add(time.Hour)}
	r.cache["c"] = &cacheEntry{vkID: "vk_drop", expiresAt: time.Now().Add(time.Hour)}
	r.InvalidateVK("vk_drop")
	if len(r.cache) != 1 {
		t.Fatalf("want 1 entry left, got %d", len(r.cache))
	}
	if _, ok := r.cache["a"]; !ok {
		t.Fatal("non-matching entry should survive")
	}
}
