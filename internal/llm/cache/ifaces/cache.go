// Package ifaces defines the semantic-cache seam: the Cache interface a driver
// implements, the Driver factory contract, and the value types that cross the
// boundary. Concrete drivers (none, inmem, redis, weaviate, qdrant) live one
// level down and self-register via internal/llm/cache.Register.
package ifaces

import (
	"context"
	"time"

	embeddingifaces "github.com/hurtener/Portico_gateway/internal/llm/cache/embeddings/ifaces"
)

// EmbeddingGenerator is re-exported so drivers depend only on the cache ifaces
// package. The dependency is one-way: embeddings/ifaces never imports this.
type EmbeddingGenerator = embeddingifaces.EmbeddingGenerator

// Mode selects exact-hash vs embedding-similarity matching.
type Mode string

const (
	// ModeExact matches on a hash of the normalized request.
	ModeExact Mode = "exact"
	// ModeSemantic matches on embedding similarity above a threshold.
	ModeSemantic Mode = "semantic"
)

// Scope is the cache-key partition level. Cross-tenant sharing is NEVER allowed
// (tenant_id is always part of the key); Scope sub-partitions within a tenant.
type Scope string

const (
	// ScopeTenant shares cache entries across all consumers in a tenant.
	ScopeTenant Scope = "tenant"
	// ScopeCustomer partitions by Customer.
	ScopeCustomer Scope = "customer"
	// ScopeTeam partitions by Team.
	ScopeTeam Scope = "team"
	// ScopeVK partitions by Virtual Key.
	ScopeVK Scope = "vk"
)

// Key identifies a cache entry. The driver hashes NormalizedInput; the composed
// string key (see internal/llm/cache.Compose) always begins with tenant_id so
// cross-tenant collisions are impossible by construction.
type Key struct {
	TenantID         string
	Scope            Scope
	ScopeID          string    // vk/team/customer id; "" for tenant scope
	Alias            string    // model alias
	NormalizedInput  []byte    // canonicalised request bytes; hashed by the driver
	SimilarityVector []float32 // optional; semantic mode
	Mode             Mode
	ExtraSalt        []byte // operator per-route salt (e.g. system-prompt fingerprint)
}

// Entry is a stored cache value (a serialised, redactor-applied response).
type Entry struct {
	Payload    []byte
	Mode       Mode
	Similarity float32 // semantic hit score; 1.0 for exact
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Tokens     int
	CostUSD    float64
}

// LookupOpts tunes a lookup. Threshold applies only in semantic mode.
type LookupOpts struct {
	Threshold float32 // semantic similarity floor (e.g. 0.85); 0 → driver default
}

// Prefix selects entries to invalidate. Invalidation never crosses tenants.
type Prefix struct {
	TenantID string // required
	Alias    string // non-empty → all entries for this alias
	ScopeID  string // non-empty → all entries for this scope id (e.g. a VK)
	All      bool   // true → all entries for the tenant
}

// Stats is a per-tenant cache snapshot for the Console stats card.
type Stats struct {
	Entries int
	HitRate float64 // 0–1 over the driver's window; 0 when unknown
	Driver  string
}

// Cache is the semantic-cache seam.
type Cache interface {
	Name() string
	// Lookup returns (entry, true, nil) on a hit. A miss is (nil, false, nil) —
	// NOT an error. The driver MUST verify the returned entry's tenant matches
	// key.TenantID before delivering (defence in depth).
	Lookup(ctx context.Context, key Key, opts LookupOpts) (*Entry, bool, error)
	// Store writes an entry under key; the driver honours entry.ExpiresAt.
	Store(ctx context.Context, key Key, entry Entry) error
	// Invalidate removes entries matching prefix; returns the count removed.
	Invalidate(ctx context.Context, prefix Prefix) (int, error)
	// Stats returns per-tenant counters.
	Stats(ctx context.Context, tenantID string) (Stats, error)
	// Close releases resources.
	Close(ctx context.Context) error
}

// Deps are cross-cutting dependencies a driver may use. All optional; none/inmem
// ignore the embedder (exact mode only).
type Deps struct {
	Embedder EmbeddingGenerator
}

// Driver builds a Cache from an opaque per-driver config block.
type Driver interface {
	Name() string
	New(cfg map[string]any, deps Deps) (Cache, error)
}
