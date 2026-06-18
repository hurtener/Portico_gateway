// Package redis is the Redis/Valkey semantic-cache driver for the §4.4 cache
// seam. It supports exact-hash mode only (semantic lookups miss): the composed
// request hash is the cache key, and Redis' own TTL handles expiry. The driver
// self-registers as "redis" via cache.Register from init(), mirroring inmem/none.
//
// Redis key layout (built here, NOT cache.Compose, so invalidation globs work):
//
//	<key_prefix>:<tenant>:<scope>:<scopeID>:<alias>:<hex(sha256(input||extraSalt))>
//
// Tenant is ALWAYS first after the prefix so invalidation never crosses tenants.
// The tenant id is also stored in the JSON value and re-checked on lookup as
// defence in depth. The driver is lazy: a down Redis must not crash boot, so the
// factory does NOT Ping — Lookup/Store surface connection errors instead.
package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

const (
	defaultAddr      = "127.0.0.1:6379"
	defaultKeyPrefix = "portico:cache"
	defaultTTL       = 5 * time.Minute
	scanBatchSize    = 100
)

func init() {
	cache.Register("redis", newDriver)
}

// newDriver builds a redis Cache from an opaque config block. It does NOT Ping
// the server: a unreachable Redis is reported lazily by Lookup/Store so a
// misconfigured cache never blocks boot.
func newDriver(cfg map[string]any, _ ifaces.Deps) (ifaces.Cache, error) {
	db := intOpt(cfg, "db", 0)
	cli := redis.NewClient(&redis.Options{
		Addr:     strOpt(cfg, "addr", defaultAddr),
		Password: strOpt(cfg, "password", ""),
		DB:       db,
	})
	return &redisCache{
		client:    cli,
		keyPrefix: strOpt(cfg, "key_prefix", defaultKeyPrefix),
		ttl:       durOpt(cfg, "default_ttl"),
	}, nil
}

// redisCache is the exact-hash Redis cache. hits/misses are per-driver counters
// surfaced via Stats.HitRate.
type redisCache struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration

	hits   int64
	misses int64
}

func (c *redisCache) Name() string { return "redis" }

func (c *redisCache) Close(_ context.Context) error {
	return c.client.Close()
}
