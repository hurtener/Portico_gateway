package ifaces

import (
	"context"
	"errors"
)

// CacheEntry is one SQLite-backed semantic-cache row. Payload is the serialised
// (redactor-applied) response. Embedding+Similarity are semantic-mode only.
type CacheEntry struct {
	TenantID   string
	CacheKey   string
	Mode       string // exact|semantic
	Alias      string
	Payload    []byte
	Embedding  []byte
	Similarity float64
	Tokens     int
	CostUSD    float64
	CreatedAt  string
	ExpiresAt  string
}

// ErrCacheEntryNotFound is returned on a cache miss.
var ErrCacheEntryNotFound = errors.New("storage: cache entry not found")

// CacheEntryStore is the SQLite-backed store the dev/test cache driver wraps.
// Tenant-scoped (§6); cross-tenant collisions impossible by PK construction.
type CacheEntryStore interface {
	PutCacheEntry(ctx context.Context, e *CacheEntry) error
	// GetCacheEntry returns the entry; ErrCacheEntryNotFound on miss. Does NOT
	// filter expired rows — the caller checks ExpiresAt (so it can record age).
	GetCacheEntry(ctx context.Context, tenantID, cacheKey string) (*CacheEntry, error)
	// DeleteByCacheKeyPrefix removes entries whose cache_key starts with prefix.
	DeleteByCacheKeyPrefix(ctx context.Context, tenantID, prefix string) (int, error)
	// DeleteByAlias removes all entries for a model alias.
	DeleteByAlias(ctx context.Context, tenantID, alias string) (int, error)
	// DeleteExpired removes entries with expires_at <= nowRFC3339.
	DeleteExpired(ctx context.Context, tenantID, nowRFC3339 string) (int, error)
	// CountEntries returns the live entry count for a tenant (stats card).
	CountEntries(ctx context.Context, tenantID string) (int, error)
}
