package redis

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// Lookup implements exact-hash lookup. Semantic mode is a miss (exact-only
// driver). redis.Nil is a clean miss, NOT an error. The stored tenant id is
// re-checked against key.TenantID as defence in depth.
func (c *redisCache) Lookup(ctx context.Context, key ifaces.Key, _ ifaces.LookupOpts) (*ifaces.Entry, bool, error) {
	if key.Mode == ifaces.ModeSemantic {
		atomic.AddInt64(&c.misses, 1)
		return nil, false, nil
	}
	rk := c.redisKey(key)

	raw, err := c.client.Get(ctx, rk).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			atomic.AddInt64(&c.misses, 1)
			return nil, false, nil
		}
		return nil, false, err
	}
	var w wireEntry
	if err := json.Unmarshal(raw, &w); err != nil {
		// Corrupted entry → miss, not a hard error (the entry is unreadable).
		atomic.AddInt64(&c.misses, 1)
		return nil, false, nil
	}
	// Defence in depth: redis key is tenant-first, but the value also carries
	// tenant and must match the request. A mismatch is a miss, never a hit.
	if w.TenantID != key.TenantID {
		atomic.AddInt64(&c.misses, 1)
		return nil, false, nil
	}
	if !w.ExpiresAt.IsZero() && w.ExpiresAt.Before(time.Now()) {
		atomic.AddInt64(&c.misses, 1)
		return nil, false, nil
	}
	e := w.fromWire()
	atomic.AddInt64(&c.hits, 1)
	return &e, true, nil
}

// Store writes an entry under key. ExpiresAt defaults to now+ttl; CreatedAt
// defaults to now. An already-expired entry is skipped (nothing to cache).
// Redis' own EX handles expiry; no manual purge is needed on lookup.
func (c *redisCache) Store(ctx context.Context, key ifaces.Key, entry ifaces.Entry) error {
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = time.Now().Add(c.ttl)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	ttl := time.Until(entry.ExpiresAt)
	if ttl <= 0 {
		// Past-due: nothing to cache.
		return nil
	}
	secs := int(ttl.Seconds())
	if secs < 1 {
		secs = 1
	}
	w := toWire(key.TenantID, entry)
	raw, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, c.redisKey(key), raw, time.Duration(secs)*time.Second).Err()
}
