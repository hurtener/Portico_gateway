package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"

	_ "github.com/hurtener/Portico_gateway/internal/llm/cache/inmem"
)

func TestInmem_ConfigParsing_StringValues(t *testing.T) {
	ctx := context.Background()
	// String forms exercise the defensive option parsers.
	c, err := cache.Open("inmem", map[string]any{"max_entries": "2", "default_ttl": "50ms"}, ifaces.Deps{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	k := keyFor("t", ifaces.ScopeTenant, "", "m", "ttl-test")
	// Store WITHOUT an explicit ExpiresAt so the default_ttl applies.
	if err := c.Store(ctx, k, ifaces.Entry{Payload: []byte("x"), Mode: ifaces.ModeExact}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); !hit {
		t.Fatalf("entry should be live immediately after store")
	}
	time.Sleep(70 * time.Millisecond)
	if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); hit {
		t.Fatalf("entry should have expired via default_ttl=50ms")
	}
}

func TestInmem_StatsHitRate(t *testing.T) {
	ctx := context.Background()
	c, _ := cache.Open("inmem", nil, ifaces.Deps{})
	k := keyFor("t", ifaces.ScopeTenant, "", "m", "x")
	_ = c.Store(ctx, k, futureEntry("x"))
	_, _, _ = c.Lookup(ctx, k, ifaces.LookupOpts{})                                                // hit
	_, _, _ = c.Lookup(ctx, keyFor("t", ifaces.ScopeTenant, "", "m", "miss"), ifaces.LookupOpts{}) // miss

	st, err := c.Stats(ctx, "t")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Entries != 1 {
		t.Errorf("entries: want 1, got %d", st.Entries)
	}
	if st.HitRate <= 0 || st.HitRate >= 1 {
		t.Errorf("hit rate should be between 0 and 1 (1 hit / 1 miss = 0.5), got %v", st.HitRate)
	}
}

func TestInmem_InvalidateEmptyTenantNoop(t *testing.T) {
	ctx := context.Background()
	c, _ := cache.Open("inmem", nil, ifaces.Deps{})
	if n, err := c.Invalidate(ctx, ifaces.Prefix{}); err != nil || n != 0 {
		t.Fatalf("empty-tenant invalidate: want (0,nil), got (%d,%v)", n, err)
	}
}

func TestNormalizeEmbeddingInput_Stable(t *testing.T) {
	a := cache.NormalizeEmbeddingInput("emb", []string{"a", "b"})
	b := cache.NormalizeEmbeddingInput("emb", []string{"a", "b"})
	if string(a) != string(b) {
		t.Fatalf("embedding input must normalize stably")
	}
	if string(a) == string(cache.NormalizeEmbeddingInput("emb", []string{"a", "c"})) {
		t.Fatalf("different input must normalize differently")
	}
}
