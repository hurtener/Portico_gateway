package profiles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type fakeStore struct {
	profile *ifaces.AgentProfile
	err     error
	calls   int
}

func (f *fakeStore) ResolveJWTBinding(_ context.Context, _, _ string) (*ifaces.AgentProfile, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.profile, nil
}

// newTestResolver builds an lruResolver with an injectable clock for
// deterministic TTL/eviction tests.
func newTestResolver(store BindingStore, ttl time.Duration, maxItems int, now func() time.Time) *lruResolver {
	if maxItems <= 0 {
		maxItems = defaultMaxItems
	}
	return &lruResolver{store: store, ttl: ttl, maxItems: maxItems, now: now, cache: map[string]cacheEntry{}}
}

func TestResolve_BoundProfile(t *testing.T) {
	store := &fakeStore{profile: &ifaces.AgentProfile{
		TenantID: "t1", ID: "ap_1", Name: "agent", AllowedMCPServers: []string{"github"},
	}}
	r := NewResolver(store, time.Minute, 10)
	p, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub-1"})
	if err != nil {
		t.Fatal(err)
	}
	if p.IsDefault || p.ID != "ap_1" || !p.AllowsServer("github") || p.AllowsServer("jira") {
		t.Fatalf("bound profile wrong: %+v", p)
	}
}

func TestResolve_NoBinding_Default(t *testing.T) {
	store := &fakeStore{err: ifaces.ErrAgentProfileNotFound}
	r := NewResolver(store, time.Minute, 10)
	p, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "unbound"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsDefault || !p.AllowsServer("anything") {
		t.Fatalf("expected allow-all default, got %+v", p)
	}
}

func TestResolve_EmptySubject_DefaultNoStoreCall(t *testing.T) {
	store := &fakeStore{}
	r := NewResolver(store, time.Minute, 10)
	p, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: ""})
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsDefault || store.calls != 0 {
		t.Fatalf("empty subject should default without a store call: default=%v calls=%d", p.IsDefault, store.calls)
	}
}

func TestResolve_StoreError_FailsClosed(t *testing.T) {
	store := &fakeStore{err: errors.New("db down")}
	r := NewResolver(store, time.Minute, 10)
	_, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub"})
	if err == nil {
		t.Fatal("a store error must fail closed (return error), not default to full surface")
	}
}

func TestResolve_CacheHit(t *testing.T) {
	store := &fakeStore{profile: &ifaces.AgentProfile{TenantID: "t1", ID: "ap_1"}}
	r := NewResolver(store, time.Minute, 10)
	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub"}); err != nil {
			t.Fatal(err)
		}
	}
	if store.calls != 1 {
		t.Fatalf("cache miss: store called %d times, want 1", store.calls)
	}
}

func TestResolve_TTLExpiry(t *testing.T) {
	store := &fakeStore{profile: &ifaces.AgentProfile{TenantID: "t1", ID: "ap_1"}}
	clock := time.Unix(1000, 0)
	r := newTestResolver(store, 60*time.Second, 10, func() time.Time { return clock })
	if _, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub"}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(61 * time.Second) // past TTL
	if _, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub"}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 2 {
		t.Fatalf("TTL did not expire: store called %d times, want 2", store.calls)
	}
}

func TestResolve_Eviction_BoundedSize(t *testing.T) {
	store := &fakeStore{err: ifaces.ErrAgentProfileNotFound}
	clock := time.Unix(1000, 0)
	r := newTestResolver(store, time.Hour, 2, func() time.Time { return clock })
	for _, sub := range []string{"a", "b", "c", "d"} {
		if _, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: sub}); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Lock()
	size := len(r.cache)
	r.mu.Unlock()
	if size > 2 {
		t.Fatalf("cache exceeded maxItems: size=%d, want <= 2", size)
	}
}

func TestInvalidate_DropsTenantEntries(t *testing.T) {
	store := &fakeStore{profile: &ifaces.AgentProfile{TenantID: "t1", ID: "ap_1"}}
	r := NewResolver(store, time.Hour, 10)
	if _, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub"}); err != nil {
		t.Fatal(err)
	}
	r.Invalidate("t1", "ap_1")
	if _, err := r.Resolve(context.Background(), Principal{TenantID: "t1", Subject: "sub"}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 2 {
		t.Fatalf("invalidate did not drop the entry: store called %d times, want 2", store.calls)
	}
}
