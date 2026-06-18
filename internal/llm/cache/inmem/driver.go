// Package inmem is a bounded-LRU in-memory semantic-cache driver for
// development and tests. It supports exact-hash mode only (semantic lookups
// miss when no embedding vector is supplied). Entries are partitioned by the
// composed key (tenant-first), and every lookup re-checks the entry's tenant as
// defence in depth.
package inmem

import (
	"container/list"
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

const (
	defaultMaxEntries = 1024
	defaultTTL        = 5 * time.Minute
)

func init() {
	cache.Register("inmem", newDriver)
}

func newDriver(cfg map[string]any, _ ifaces.Deps) (ifaces.Cache, error) {
	return &memCache{
		maxEntries: intOpt(cfg, "max_entries", defaultMaxEntries),
		ttl:        durOpt(cfg, "default_ttl", defaultTTL),
		items:      map[string]*list.Element{},
		ll:         list.New(),
	}, nil
}

// node is the value stored in each LRU list element.
type node struct {
	key      string // composed key
	tenantID string
	scope    ifaces.Scope
	scopeID  string
	alias    string
	entry    ifaces.Entry
}

type memCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	items      map[string]*list.Element // composed key -> element
	ll         *list.List               // front = most recently used
	hits       int64
	misses     int64
}

func (c *memCache) Name() string { return "inmem" }

func (c *memCache) Lookup(_ context.Context, key ifaces.Key, _ ifaces.LookupOpts) (*ifaces.Entry, bool, error) {
	// Semantic lookups need a vector store; this driver is exact-only.
	if key.Mode == ifaces.ModeSemantic {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false, nil
	}
	ck := cache.Compose(key)

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[ck]
	if !ok {
		c.misses++
		return nil, false, nil
	}
	n := nodeOf(el)
	// Expiry → lazy purge.
	if !n.entry.ExpiresAt.IsZero() && n.entry.ExpiresAt.Before(time.Now()) {
		c.removeElement(el)
		c.misses++
		return nil, false, nil
	}
	// Defence in depth: the composed key is tenant-first, but re-check anyway.
	if n.tenantID != key.TenantID {
		c.misses++
		return nil, false, nil
	}
	c.ll.MoveToFront(el)
	c.hits++
	cp := n.entry
	return &cp, true, nil
}

func (c *memCache) Store(_ context.Context, key ifaces.Key, entry ifaces.Entry) error {
	ck := cache.Compose(key)
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = time.Now().Add(c.ttl)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[ck]; ok {
		n := nodeOf(el)
		n.entry = entry
		c.ll.MoveToFront(el)
		return nil
	}
	n := &node{
		key:      ck,
		tenantID: key.TenantID,
		scope:    key.Scope,
		scopeID:  key.ScopeID,
		alias:    key.Alias,
		entry:    entry,
	}
	c.items[ck] = c.ll.PushFront(n)
	c.evictLocked()
	return nil
}

func (c *memCache) Invalidate(_ context.Context, prefix ifaces.Prefix) (int, error) {
	if prefix.TenantID == "" {
		return 0, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	var removed int
	for el := c.ll.Back(); el != nil; {
		prev := el.Prev()
		n := nodeOf(el)
		if n.tenantID == prefix.TenantID && matchPrefix(n, prefix) {
			c.removeElement(el)
			removed++
		}
		el = prev
	}
	return removed, nil
}

// matchPrefix reports whether node n (already tenant-matched) is selected by
// prefix. Exactly one of All/Alias/ScopeID is expected to be set; All wins.
func matchPrefix(n *node, p ifaces.Prefix) bool {
	switch {
	case p.All:
		return true
	case p.Alias != "":
		return n.alias == p.Alias
	case p.ScopeID != "":
		return n.scopeID == p.ScopeID
	default:
		// No selector beyond tenant → treat as "all for tenant".
		return true
	}
}

func (c *memCache) Stats(_ context.Context, tenantID string) (ifaces.Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entries := 0
	for el := c.ll.Front(); el != nil; el = el.Next() {
		n := nodeOf(el)
		if n.tenantID != tenantID {
			continue
		}
		if !n.entry.ExpiresAt.IsZero() && n.entry.ExpiresAt.Before(now) {
			continue
		}
		entries++
	}
	var hitRate float64
	if total := c.hits + c.misses; total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}
	return ifaces.Stats{Entries: entries, HitRate: hitRate, Driver: "inmem"}, nil
}

func (c *memCache) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[string]*list.Element{}
	c.ll.Init()
	return nil
}

// evictLocked drops least-recently-used entries until within capacity.
func (c *memCache) evictLocked() {
	if c.maxEntries <= 0 {
		return
	}
	for c.ll.Len() > c.maxEntries {
		if el := c.ll.Back(); el != nil {
			c.removeElement(el)
		} else {
			return
		}
	}
}

// nodeOf returns the *node stored in an LRU element. The list only ever holds
// *node values, so the assertion cannot fail; the comma-ok form keeps the
// type-assertion lint happy.
func nodeOf(el *list.Element) *node {
	n, _ := el.Value.(*node)
	return n
}

func (c *memCache) removeElement(el *list.Element) {
	c.ll.Remove(el)
	delete(c.items, nodeOf(el).key)
}

// --- config option parsing (defensive: accept int/float64/string) ---

func intOpt(cfg map[string]any, key string, def int) int {
	v, ok := cfg[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case int:
		if t > 0 {
			return t
		}
	case int64:
		if t > 0 {
			return int(t)
		}
	case float64:
		if t > 0 {
			return int(t)
		}
	case string:
		if n, err := strconv.Atoi(t); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func durOpt(cfg map[string]any, key string, def time.Duration) time.Duration {
	v, ok := cfg[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case time.Duration:
		if t > 0 {
			return t
		}
	case string:
		if d, err := time.ParseDuration(t); err == nil && d > 0 {
			return d
		}
	case int:
		if t > 0 {
			return time.Duration(t) * time.Second
		}
	case float64:
		if t > 0 {
			return time.Duration(t) * time.Second
		}
	}
	return def
}
