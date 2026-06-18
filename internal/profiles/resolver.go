package profiles

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Principal is the post-auth identity a request is resolved for. Subject is the
// JWT `sub` claim (or the user id as a fallback); the empty Subject resolves to
// the default profile.
type Principal struct {
	TenantID string
	Subject  string
}

// Resolver maps a principal to its bound profile, with caching. It is the single
// entry point the profile middleware calls.
type Resolver interface {
	// Resolve returns the profile bound to the principal, or the synthesised
	// default profile when the principal has no binding. It returns a non-nil
	// error only on an underlying store failure (the caller fails closed).
	Resolve(ctx context.Context, p Principal) (*Profile, error)
	// Invalidate drops cached entries for a tenant's profile after a write. A
	// coarse tenant-wide flush is acceptable: the TTL bounds staleness anyway.
	Invalidate(tenantID, profileID string)
}

// BindingStore is the slice of ifaces.AgentProfileStore the resolver needs.
type BindingStore interface {
	ResolveJWTBinding(ctx context.Context, tenantID, jwtSub string) (*ifaces.AgentProfile, error)
}

const (
	defaultTTL      = 60 * time.Second
	defaultMaxItems = 1024
)

type cacheEntry struct {
	profile   *Profile
	expiresAt time.Time
}

type lruResolver struct {
	store    BindingStore
	ttl      time.Duration
	maxItems int
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
	order []string // FIFO insertion order for bounded eviction
}

// NewResolver builds a caching resolver over the store. ttl <= 0 and maxItems
// <= 0 normalise to the conservative defaults (60s, 1024 entries).
func NewResolver(store BindingStore, ttl time.Duration, maxItems int) Resolver {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if maxItems <= 0 {
		maxItems = defaultMaxItems
	}
	return &lruResolver{
		store:    store,
		ttl:      ttl,
		maxItems: maxItems,
		now:      time.Now,
		cache:    make(map[string]cacheEntry, maxItems),
	}
}

func key(tenantID, subject string) string { return tenantID + "|" + subject }

func (r *lruResolver) Resolve(ctx context.Context, p Principal) (*Profile, error) {
	// No tenant → nothing to resolve; default (allow-all) keeps non-tenant or
	// dev paths working.
	if p.TenantID == "" {
		return DefaultProfile(""), nil
	}
	k := key(p.TenantID, p.Subject)
	if prof := r.cached(k); prof != nil {
		return prof, nil
	}

	// An empty subject can't be bound; it resolves to the default without a
	// store round-trip.
	if p.Subject == "" {
		prof := DefaultProfile(p.TenantID)
		r.putCache(k, prof)
		return prof, nil
	}

	ap, err := r.store.ResolveJWTBinding(ctx, p.TenantID, p.Subject)
	switch {
	case errors.Is(err, ifaces.ErrAgentProfileNotFound):
		prof := DefaultProfile(p.TenantID)
		r.putCache(k, prof)
		return prof, nil
	case err != nil:
		// Fail closed: we could not determine entitlement. The caller (middleware)
		// surfaces this as a 503 rather than defaulting to full surface.
		return nil, err
	default:
		prof := fromStore(ap)
		r.putCache(k, prof)
		return prof, nil
	}
}

func (r *lruResolver) cached(k string) *Profile {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[k]
	if !ok {
		return nil
	}
	if r.now().After(e.expiresAt) {
		delete(r.cache, k)
		return nil
	}
	return e.profile
}

// putCache inserts into the bounded cache, evicting the oldest entry (FIFO) when
// over capacity.
func (r *lruResolver) putCache(k string, prof *Profile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cache[k]; !exists {
		r.order = append(r.order, k)
	}
	r.cache[k] = cacheEntry{profile: prof, expiresAt: r.now().Add(r.ttl)}
	for len(r.cache) > r.maxItems && len(r.order) > 0 {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.cache, oldest)
	}
}

func (r *lruResolver) Invalidate(tenantID, profileID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, e := range r.cache {
		if e.profile == nil {
			continue
		}
		if e.profile.TenantID == tenantID && (profileID == "" || e.profile.ID == profileID || e.profile.IsDefault) {
			delete(r.cache, k)
		}
	}
}
