package redis

import (
	"context"
	"sync/atomic"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// Invalidate removes entries matching prefix via SCAN + MATCH (never KEYS) then
// a single DEL of the collected keys. Tenant is ALWAYS in the pattern; an empty
// TenantID is a no-op (invalidation never crosses tenants). Returns the count
// removed.
func (c *redisCache) Invalidate(ctx context.Context, prefix ifaces.Prefix) (int, error) {
	if prefix.TenantID == "" {
		return 0, nil
	}
	keys, err := c.scanKeys(ctx, c.matchPattern(prefix))
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	n, err := c.client.Del(ctx, keys...).Result()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// Stats reports this tenant's live key count (via SCAN + MATCH) and a hit rate
// from the driver's atomic hits/misses counters. Driver is always "redis".
func (c *redisCache) Stats(ctx context.Context, tenantID string) (ifaces.Stats, error) {
	if tenantID == "" {
		return ifaces.Stats{Driver: "redis"}, nil
	}
	keys, err := c.scanKeys(ctx, c.statsPattern(tenantID))
	if err != nil {
		return ifaces.Stats{}, err
	}
	hits := atomic.LoadInt64(&c.hits)
	misses := atomic.LoadInt64(&c.misses)
	var hitRate float64
	if total := hits + misses; total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	return ifaces.Stats{
		Entries: len(keys),
		HitRate: hitRate,
		Driver:  "redis",
	}, nil
}
