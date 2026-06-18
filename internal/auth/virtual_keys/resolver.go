package virtualkeys

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Sentinel resolution errors. ErrUnknown is deliberately AMBIGUOUS (returned for
// malformed tokens, missing VKs, and HMAC mismatches alike) so an attacker
// cannot enumerate valid VK ids or tenants. ErrRevoked is distinct so a
// legitimate caller learns their key was revoked (vs never existed).
var (
	ErrUnknown = errors.New("virtualkeys: unknown or invalid key")
	ErrRevoked = errors.New("virtualkeys: key revoked")
)

// defaultTTL is how long a verified resolution is cached. Short, so revocation
// at another instance takes effect quickly; same-instance revoke/rotate also
// call InvalidateVK for immediate effect (acceptance #14/#15).
const defaultTTL = 60 * time.Second

// maxCacheEntries bounds the resolver cache.
const maxCacheEntries = 4096

// Resolver turns a Bearer "pk-portico-…" string into a *Resolved. It verifies
// the HMAC against the stored salt+hmac and caches verified results briefly. The
// VK id lookup is the documented auth-boundary path (a presented VK carries no
// tenant; the id is globally unique).
type Resolver struct {
	store ifaces.GovernanceStore
	ttl   time.Duration

	mu    sync.Mutex
	cache map[string]*cacheEntry // key = sha256(token) hex
}

type cacheEntry struct {
	resolved  *Resolved
	vkID      string
	expiresAt time.Time
}

// NewResolver builds a resolver over the governance store. ttl<=0 uses the
// default 60s.
func NewResolver(store ifaces.GovernanceStore, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Resolver{store: store, ttl: ttl, cache: map[string]*cacheEntry{}}
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Resolve verifies a VK bearer token and returns its hydrated state. Malformed
// tokens are rejected without a DB hit. A verified result is cached for ttl.
func (r *Resolver) Resolve(ctx context.Context, token string) (*Resolved, error) {
	id, secret, err := ParseToken(token)
	if err != nil {
		return nil, ErrUnknown
	}
	h := tokenHash(token)

	// Cache hit (verified earlier).
	r.mu.Lock()
	if e, ok := r.cache[h]; ok {
		if time.Now().Before(e.expiresAt) {
			res := e.resolved
			r.mu.Unlock()
			return res, nil
		}
		delete(r.cache, h)
	}
	r.mu.Unlock()

	vk, err := r.store.LookupVirtualKeyByID(ctx, id)
	if err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return nil, ErrUnknown
		}
		return nil, err // genuine store error — fail closed upstream
	}
	// Verify the secret against this VK's stored salt+hmac (constant time).
	if !VerifyHMAC(vk.Salt, vk.HMAC, secret) {
		return nil, ErrUnknown
	}
	if !vk.Enabled || vk.RevokedAt != "" {
		return nil, ErrRevoked
	}

	resolved := &Resolved{
		VKID:               vk.ID,
		TenantID:           vk.TenantID,
		Name:               vk.Name,
		Scopes:             vk.Scopes,
		ProviderAllowlist:  vk.ProviderAllowlist,
		ModelAllowlist:     vk.ModelAllowlist,
		MCPServerAllowlist: vk.MCPServerAllowlist,
		ProfileID:          vk.ProfileID,
		ParentKind:         vk.ParentKind,
		ParentID:           vk.ParentID,
	}

	r.mu.Lock()
	if len(r.cache) >= maxCacheEntries {
		r.evictOneLocked()
	}
	r.cache[h] = &cacheEntry{resolved: resolved, vkID: vk.ID, expiresAt: time.Now().Add(r.ttl)}
	r.mu.Unlock()
	return resolved, nil
}

// InvalidateVK drops every cached resolution for a VK id (called on
// revoke/rotate so the change is effective immediately on this instance).
func (r *Resolver) InvalidateVK(vkID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for h, e := range r.cache {
		if e.vkID == vkID {
			delete(r.cache, h)
		}
	}
}

// evictOneLocked removes an arbitrary (preferably expired) entry to bound the
// cache. Caller holds r.mu.
func (r *Resolver) evictOneLocked() {
	now := time.Now()
	for h, e := range r.cache {
		if now.After(e.expiresAt) {
			delete(r.cache, h)
			return
		}
	}
	// No expired entry — drop an arbitrary one (map iteration order is random).
	for h := range r.cache {
		delete(r.cache, h)
		return
	}
}

// nowRFC3339 is the timestamp format the stores use.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
