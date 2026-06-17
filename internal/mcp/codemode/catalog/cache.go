package catalog

import (
	"sync"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

// ProjectionCache memoizes projections by (snapshot id, binding level). A
// snapshot is immutable for its lifetime (Phase 6), so a cached projection never
// goes stale; entries are evicted only when the cache is full or a tenant's
// snapshot rotates (the caller drops the old id). The cache is safe for
// concurrent use.
//
// It is a defense against re-rendering a 500-tool snapshot on every
// listToolFiles/readToolFile call (a documented performance pitfall), not a
// correctness mechanism — Project is pure, so a cache miss is always safe.
type ProjectionCache struct {
	mu      sync.Mutex
	entries map[string]Projection
	order   []string // insertion order for bounded FIFO eviction
	max     int
}

// DefaultProjectionCacheSize bounds the number of cached projections.
const DefaultProjectionCacheSize = 64

// NewProjectionCache returns a cache holding up to max projections (FIFO
// eviction). A non-positive max uses DefaultProjectionCacheSize.
func NewProjectionCache(max int) *ProjectionCache {
	if max <= 0 {
		max = DefaultProjectionCacheSize
	}
	return &ProjectionCache{
		entries: make(map[string]Projection, max),
		max:     max,
	}
}

// Get returns the projection for snap at level, computing and caching it on a
// miss. A nil snapshot is projected but not cached (no stable key).
func (c *ProjectionCache) Get(snap *snapshots.Snapshot, level BindingLevel) Projection {
	if snap == nil || snap.ID == "" {
		return Project(snap, level)
	}
	key := snap.ID + "|" + string(level)

	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.entries[key]; ok {
		return p
	}
	p := Project(snap, level)
	c.put(key, p)
	return p
}

// Invalidate drops every cached level for a snapshot id (called on snapshot
// rotation for a tenant).
func (c *ProjectionCache) Invalidate(snapshotID string) {
	if snapshotID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, level := range []BindingLevel{BindingServer, BindingTool} {
		key := snapshotID + "|" + string(level)
		if _, ok := c.entries[key]; ok {
			delete(c.entries, key)
			c.removeOrder(key)
		}
	}
}

// put inserts under lock, evicting the oldest entry if at capacity.
func (c *ProjectionCache) put(key string, p Projection) {
	if _, exists := c.entries[key]; !exists {
		if len(c.order) >= c.max {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.entries, oldest)
		}
		c.order = append(c.order, key)
	}
	c.entries[key] = p
}

func (c *ProjectionCache) removeOrder(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}
