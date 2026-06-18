package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// newCache starts a miniredis and returns a redis Cache pointed at it, the
// miniredis handle (for FastForward / direct inspection), and a cleanup.
// The driver self-registers from init(), so cache.Open("redis", ...) dispatches
// to newDriver without a blank import.
func newCache(t *testing.T) (ifaces.Cache, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	c, err := cache.Open("redis", map[string]any{"addr": mr.Addr()}, ifaces.Deps{})
	if err != nil {
		t.Fatalf("cache.Open redis: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c, mr
}

func keyFor(tenant string, scope ifaces.Scope, scopeID, alias, prompt string) ifaces.Key {
	return ifaces.Key{
		TenantID:        tenant,
		Scope:           scope,
		ScopeID:         scopeID,
		Alias:           alias,
		Mode:            ifaces.ModeExact,
		NormalizedInput: cache.NormalizeChatInput(alias, []cache.NormMessage{{Role: "user", Content: prompt}}, nil, nil),
	}
}

func entry(payload string) ifaces.Entry {
	return ifaces.Entry{
		Payload:   []byte(payload),
		Mode:      ifaces.ModeExact,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Tokens:    10,
		CostUSD:   0.001,
	}
}

func TestRedis_ExactHitAndMiss(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)

	k := keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "gpt-4", "hello world")
	if err := c.Store(ctx, k, entry("RESP")); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, hit, err := c.Lookup(ctx, k, ifaces.LookupOpts{})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !hit || string(got.Payload) != "RESP" {
		t.Fatalf("want exact hit RESP, hit=%v got=%+v", hit, got)
	}
	if got.Tokens != 10 || got.CostUSD != 0.001 {
		t.Fatalf("metadata not round-tripped: tokens=%d cost=%v", got.Tokens, got.CostUSD)
	}

	// A different request (different prompt) → miss.
	k2 := keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "gpt-4", "a different prompt")
	if _, hit, err := c.Lookup(ctx, k2, ifaces.LookupOpts{}); err != nil {
		t.Fatalf("lookup miss: %v", err)
	} else if hit {
		t.Fatalf("different request should miss")
	}
}

func TestRedis_PerTenantIsolation(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)

	if err := c.Store(ctx, keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "gpt-4", "shared prompt"), entry("A")); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Same logical request under a different tenant must miss — and the redis
	// key is tenant-first so the GET lands on a different (absent) key.
	kb := keyFor("tenant-b", ifaces.ScopeVK, "vk-1", "gpt-4", "shared prompt")
	if _, hit, err := c.Lookup(ctx, kb, ifaces.LookupOpts{}); err != nil {
		t.Fatalf("lookup: %v", err)
	} else if hit {
		t.Fatalf("cross-tenant leak: tenant-b got a hit on tenant-a's entry")
	}
}

func TestRedis_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	c, mr := newCache(t)

	// Short TTL: store, then advance miniredis time past expiry → miss.
	k := keyFor("tenant-a", ifaces.ScopeTenant, "", "gpt-4", "expires soon")
	e := entry("SHORT")
	e.ExpiresAt = time.Now().Add(2 * time.Second)
	if err := c.Store(ctx, k, e); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Sanity: present before fast-forward.
	if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); !hit {
		t.Fatalf("entry should be present before FastForward")
	}
	mr.FastForward(3 * time.Second)
	if _, hit, err := c.Lookup(ctx, k, ifaces.LookupOpts{}); err != nil {
		t.Fatalf("lookup after expiry: %v", err)
	} else if hit {
		t.Fatalf("expired entry should miss after FastForward")
	}
}

// seedThree writes three entries for tenant-a across two VKs and two aliases,
// plus one entry for tenant-b that must NEVER be touched by tenant-a's
// invalidations. Returns the four keys in order.
func seedThree(t *testing.T, c ifaces.Cache) []ifaces.Key {
	t.Helper()
	ctx := context.Background()
	keys := []ifaces.Key{
		keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "gpt-4", "A"),  // alias gpt-4,  vk-1
		keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "claude", "B"), // alias claude, vk-1
		keyFor("tenant-a", ifaces.ScopeVK, "vk-2", "gpt-4", "C"),  // alias gpt-4,  vk-2
		keyFor("tenant-b", ifaces.ScopeVK, "vk-1", "gpt-4", "D"),  // other tenant
	}
	for _, k := range keys {
		if err := c.Store(ctx, k, entry("x")); err != nil {
			t.Fatalf("seed store: %v", err)
		}
	}
	return keys
}

func TestRedis_InvalidateByAlias(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)
	ks := seedThree(t, c)

	n, err := c.Invalidate(ctx, ifaces.Prefix{TenantID: "tenant-a", Alias: "gpt-4"})
	if err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if n != 2 { // ks[0] + ks[2]
		t.Fatalf("invalidate by alias: want 2 removed, got %d", n)
	}
	if _, hit, _ := c.Lookup(ctx, ks[0], ifaces.LookupOpts{}); hit {
		t.Fatalf("ks[0] survived alias invalidation")
	}
	if _, hit, _ := c.Lookup(ctx, ks[2], ifaces.LookupOpts{}); hit {
		t.Fatalf("ks[2] survived alias invalidation")
	}
	// Non-matching alias and other tenant survive.
	if _, hit, _ := c.Lookup(ctx, ks[1], ifaces.LookupOpts{}); !hit {
		t.Fatalf("ks[1] (claude) should survive gpt-4 invalidation")
	}
	if _, hit, _ := c.Lookup(ctx, ks[3], ifaces.LookupOpts{}); !hit {
		t.Fatalf("ks[3] (tenant-b) should survive tenant-a invalidation")
	}
}

func TestRedis_InvalidateByScopeID(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)
	ks := seedThree(t, c)

	n, err := c.Invalidate(ctx, ifaces.Prefix{TenantID: "tenant-a", ScopeID: "vk-1"})
	if err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if n != 2 { // ks[0] + ks[1]
		t.Fatalf("invalidate by scope: want 2 removed, got %d", n)
	}
	if _, hit, _ := c.Lookup(ctx, ks[0], ifaces.LookupOpts{}); hit {
		t.Fatalf("ks[0] survived scope invalidation")
	}
	if _, hit, _ := c.Lookup(ctx, ks[1], ifaces.LookupOpts{}); hit {
		t.Fatalf("ks[1] survived scope invalidation")
	}
	if _, hit, _ := c.Lookup(ctx, ks[2], ifaces.LookupOpts{}); !hit {
		t.Fatalf("ks[2] (vk-2) should survive vk-1 invalidation")
	}
	if _, hit, _ := c.Lookup(ctx, ks[3], ifaces.LookupOpts{}); !hit {
		t.Fatalf("ks[3] (tenant-b) should survive tenant-a invalidation")
	}
}

func TestRedis_InvalidateAll(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)
	ks := seedThree(t, c)

	n, err := c.Invalidate(ctx, ifaces.Prefix{TenantID: "tenant-a", All: true})
	if err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if n != 3 { // ks[0..2]
		t.Fatalf("invalidate all: want 3 removed, got %d", n)
	}
	for i, k := range ks[:3] {
		if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); hit {
			t.Fatalf("ks[%d] survived All invalidation", i)
		}
	}
	// Tenant-only prefix (no All/Alias/ScopeID) behaves as All-for-tenant.
	if _, hit, _ := c.Lookup(ctx, ks[3], ifaces.LookupOpts{}); !hit {
		t.Fatalf("ks[3] (tenant-b) should survive tenant-a All invalidation")
	}
}

func TestRedis_InvalidateEmptyTenantIsNoop(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)
	_ = seedThree(t, c)
	n, err := c.Invalidate(ctx, ifaces.Prefix{TenantID: "", All: true})
	if err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if n != 0 {
		t.Fatalf("empty tenant invalidate: want 0, got %d", n)
	}
}

func TestRedis_SemanticModeMisses(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)
	k := keyFor("tenant-a", ifaces.ScopeTenant, "", "gpt-4", "p")
	k.Mode = ifaces.ModeSemantic
	if err := c.Store(ctx, k, entry("x")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, hit, err := c.Lookup(ctx, k, ifaces.LookupOpts{}); err != nil {
		t.Fatalf("lookup: %v", err)
	} else if hit {
		t.Fatalf("redis semantic lookup should miss (exact-only driver)")
	}
}

func TestRedis_Stats(t *testing.T) {
	ctx := context.Background()
	c, _ := newCache(t)
	_ = seedThree(t, c)

	// Three tenant-a entries; tenant-b has one.
	st, err := c.Stats(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Driver != "redis" {
		t.Fatalf("stats driver: want redis, got %q", st.Driver)
	}
	if st.Entries != 3 {
		t.Fatalf("stats entries: want 3, got %d", st.Entries)
	}
	stB, err := c.Stats(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("stats tenant-b: %v", err)
	}
	if stB.Entries != 1 {
		t.Fatalf("stats tenant-b entries: want 1, got %d", stB.Entries)
	}
}
