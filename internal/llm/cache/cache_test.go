package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"

	// Self-register the drivers under test.
	_ "github.com/hurtener/Portico_gateway/internal/llm/cache/inmem"
	_ "github.com/hurtener/Portico_gateway/internal/llm/cache/none"
)

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

func futureEntry(payload string) ifaces.Entry {
	return ifaces.Entry{
		Payload:   []byte(payload),
		Mode:      ifaces.ModeExact,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
		Tokens:    10,
	}
}

func TestOpen_UnknownDriver(t *testing.T) {
	if _, err := cache.Open("does-not-exist", nil, ifaces.Deps{}); err == nil {
		t.Fatal("want error for unknown driver")
	}
}

func TestOpen_EmptyDefaultsToNone(t *testing.T) {
	c, err := cache.Open("", nil, ifaces.Deps{})
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	if c.Name() != "none" {
		t.Fatalf("empty driver should default to none, got %q", c.Name())
	}
}

// TestDriverMatrix runs the canonical scenarios against every driver. "none"
// always misses by design; "inmem" caches.
func TestDriverMatrix(t *testing.T) {
	for _, name := range []string{"none", "inmem"} {
		name := name
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			c, err := cache.Open(name, nil, ifaces.Deps{})
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer func() { _ = c.Close(ctx) }()

			caches := name != "none" // does this driver actually store?

			k := keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "gpt-4", "hello world")
			if err := c.Store(ctx, k, futureEntry("RESP")); err != nil {
				t.Fatalf("store: %v", err)
			}

			// Exact hit (or miss for none).
			got, hit, err := c.Lookup(ctx, k, ifaces.LookupOpts{})
			if err != nil {
				t.Fatalf("lookup: %v", err)
			}
			if caches {
				if !hit || string(got.Payload) != "RESP" {
					t.Fatalf("%s: want exact hit RESP, hit=%v got=%v", name, hit, got)
				}
			} else if hit {
				t.Fatalf("none: expected miss, got hit")
			}

			// Per-tenant isolation: same logical request, different tenant → miss.
			kb := keyFor("tenant-b", ifaces.ScopeVK, "vk-1", "gpt-4", "hello world")
			if _, hit, _ := c.Lookup(ctx, kb, ifaces.LookupOpts{}); hit {
				t.Fatalf("%s: cross-tenant leak — tenant-b got a hit", name)
			}

			// TTL expiry: an already-expired entry is a miss.
			ke := keyFor("tenant-a", ifaces.ScopeVK, "vk-1", "gpt-4", "expired one")
			expired := futureEntry("OLD")
			expired.ExpiresAt = time.Now().Add(-time.Second)
			if err := c.Store(ctx, ke, expired); err != nil {
				t.Fatalf("store expired: %v", err)
			}
			if _, hit, _ := c.Lookup(ctx, ke, ifaces.LookupOpts{}); hit {
				t.Fatalf("%s: expired entry returned a hit", name)
			}

			// Invalidation by alias.
			n, err := c.Invalidate(ctx, ifaces.Prefix{TenantID: "tenant-a", Alias: "gpt-4"})
			if err != nil {
				t.Fatalf("invalidate: %v", err)
			}
			if caches && n == 0 {
				t.Fatalf("inmem: invalidate by alias removed nothing")
			}
			if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); hit {
				t.Fatalf("%s: entry survived alias invalidation", name)
			}

			// Stats never errors and reports the driver name.
			st, err := c.Stats(ctx, "tenant-a")
			if err != nil {
				t.Fatalf("stats: %v", err)
			}
			if st.Driver != name {
				t.Fatalf("stats driver: want %q, got %q", name, st.Driver)
			}
		})
	}
}

func TestInmem_InvalidateByScopeAndAll(t *testing.T) {
	ctx := context.Background()
	c, err := cache.Open("inmem", nil, ifaces.Deps{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	store := func(scopeID, alias, prompt string) {
		if err := c.Store(ctx, keyFor("t", ifaces.ScopeVK, scopeID, alias, prompt), futureEntry("x")); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	store("vk-1", "gpt-4", "a")
	store("vk-1", "claude", "b")
	store("vk-2", "gpt-4", "c")

	// Invalidate by scope id removes only vk-1's two entries.
	n, err := c.Invalidate(ctx, ifaces.Prefix{TenantID: "t", ScopeID: "vk-1"})
	if err != nil {
		t.Fatalf("invalidate scope: %v", err)
	}
	if n != 2 {
		t.Fatalf("invalidate by scope: want 2 removed, got %d", n)
	}

	// All removes the remaining vk-2 entry.
	n, err = c.Invalidate(ctx, ifaces.Prefix{TenantID: "t", All: true})
	if err != nil {
		t.Fatalf("invalidate all: %v", err)
	}
	if n != 1 {
		t.Fatalf("invalidate all: want 1 removed, got %d", n)
	}
}

func TestInmem_LRUEviction(t *testing.T) {
	ctx := context.Background()
	c, err := cache.Open("inmem", map[string]any{"max_entries": 2}, ifaces.Deps{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	k1 := keyFor("t", ifaces.ScopeTenant, "", "m", "one")
	k2 := keyFor("t", ifaces.ScopeTenant, "", "m", "two")
	k3 := keyFor("t", ifaces.ScopeTenant, "", "m", "three")
	_ = c.Store(ctx, k1, futureEntry("1"))
	_ = c.Store(ctx, k2, futureEntry("2"))
	_ = c.Store(ctx, k3, futureEntry("3")) // evicts k1 (LRU)

	if _, hit, _ := c.Lookup(ctx, k1, ifaces.LookupOpts{}); hit {
		t.Fatalf("k1 should have been evicted")
	}
	if _, hit, _ := c.Lookup(ctx, k3, ifaces.LookupOpts{}); !hit {
		t.Fatalf("k3 should be present")
	}
}

func TestInmem_SemanticModeMisses(t *testing.T) {
	ctx := context.Background()
	c, _ := cache.Open("inmem", nil, ifaces.Deps{})
	k := keyFor("t", ifaces.ScopeTenant, "", "m", "p")
	k.Mode = ifaces.ModeSemantic
	if err := c.Store(ctx, k, futureEntry("x")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); hit {
		t.Fatalf("inmem semantic lookup should miss (exact-only driver)")
	}
}
