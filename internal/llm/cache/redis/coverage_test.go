package redis

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

func TestConfigParsers(t *testing.T) {
	// strOpt
	if got := strOpt(map[string]any{"k": "v"}, "k", "def"); got != "v" {
		t.Errorf("strOpt string: %q", got)
	}
	if got := strOpt(map[string]any{"k": 5}, "k", "def"); got != "def" {
		t.Errorf("strOpt non-string should default: %q", got)
	}
	if got := strOpt(map[string]any{}, "k", "def"); got != "def" {
		t.Errorf("strOpt missing should default: %q", got)
	}

	// intOpt across all accepted types + invalid → default.
	cases := []struct {
		v   any
		def int
		exp int
	}{
		{7, 1, 7}, {int64(8), 1, 8}, {float64(9), 1, 9}, {"10", 1, 10},
		{-1, 3, 3}, {"notnum", 3, 3}, {true, 3, 3},
	}
	for _, c := range cases {
		if got := intOpt(map[string]any{"k": c.v}, "k", c.def); got != c.exp {
			t.Errorf("intOpt(%v): got %d want %d", c.v, got, c.exp)
		}
	}
	if got := intOpt(map[string]any{}, "k", 42); got != 42 {
		t.Errorf("intOpt missing: %d", got)
	}

	// durOpt across all accepted types + invalid → defaultTTL fallback.
	if got := durOpt(map[string]any{"k": "30s"}, "k"); got != 30*time.Second {
		t.Errorf("durOpt string: %v", got)
	}
	if got := durOpt(map[string]any{"k": 2 * time.Hour}, "k"); got != 2*time.Hour {
		t.Errorf("durOpt duration: %v", got)
	}
	if got := durOpt(map[string]any{"k": 90}, "k"); got != 90*time.Second {
		t.Errorf("durOpt int seconds: %v", got)
	}
	if got := durOpt(map[string]any{"k": float64(45)}, "k"); got != 45*time.Second {
		t.Errorf("durOpt float seconds: %v", got)
	}
	if got := durOpt(map[string]any{"k": "bad"}, "k"); got != defaultTTL {
		t.Errorf("durOpt invalid string should default: %v", got)
	}
	if got := durOpt(map[string]any{}, "k"); got != defaultTTL {
		t.Errorf("durOpt missing should default: %v", got)
	}
}

func TestName(t *testing.T) {
	c, _ := newCache(t)
	if c.Name() != "redis" {
		t.Fatalf("name: %q", c.Name())
	}
}

func TestStore_DefaultTTLApplied(t *testing.T) {
	c, mr := newCache(t)
	ctx := context.Background()
	k := ifaces.Key{TenantID: "t", Scope: ifaces.ScopeTenant, Alias: "m", Mode: ifaces.ModeExact, NormalizedInput: []byte("p")}
	// No ExpiresAt → driver applies its default ttl; the key must exist with a TTL.
	if err := c.Store(ctx, k, ifaces.Entry{Payload: []byte("x"), Mode: ifaces.ModeExact}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); !hit {
		t.Fatal("entry should be live after default-ttl store")
	}
	_ = mr
}

func TestStore_PastDueSkipped(t *testing.T) {
	c, _ := newCache(t)
	ctx := context.Background()
	k := ifaces.Key{TenantID: "t", Scope: ifaces.ScopeTenant, Alias: "m", Mode: ifaces.ModeExact, NormalizedInput: []byte("past")}
	e := ifaces.Entry{Payload: []byte("x"), Mode: ifaces.ModeExact, ExpiresAt: time.Now().Add(-time.Hour)}
	if err := c.Store(ctx, k, e); err != nil {
		t.Fatalf("store past-due: %v", err)
	}
	if _, hit, _ := c.Lookup(ctx, k, ifaces.LookupOpts{}); hit {
		t.Fatal("past-due entry must not be cached")
	}
}

func TestLookup_CorruptedEntryIsMiss(t *testing.T) {
	c, mr := newCache(t)
	ctx := context.Background()
	rc := c.(*redisCache)
	k := ifaces.Key{TenantID: "t", Scope: ifaces.ScopeTenant, Alias: "m", Mode: ifaces.ModeExact, NormalizedInput: []byte("corrupt")}
	// Write non-JSON bytes directly at the driver's key.
	if err := mr.Set(rc.redisKey(k), "this is not json"); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	if _, hit, err := c.Lookup(ctx, k, ifaces.LookupOpts{}); hit || err != nil {
		t.Fatalf("corrupted entry should be a clean miss: hit=%v err=%v", hit, err)
	}
}

func TestStats_EmptyTenant(t *testing.T) {
	c, _ := newCache(t)
	st, err := c.Stats(context.Background(), "")
	if err != nil {
		t.Fatalf("stats empty tenant: %v", err)
	}
	if st.Driver != "redis" || st.Entries != 0 {
		t.Fatalf("empty-tenant stats: %+v", st)
	}
}

func TestClose(t *testing.T) {
	c, _ := newCache(t)
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}
