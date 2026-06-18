package redis

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// redisKey builds the stored key from a ifaces.Key. The digest is computed over
// NormalizedInput then ExtraSalt — identical to cache.Compose — so two
// logically-equal requests hash equal across drivers. Layout:
//
//	<key_prefix>:<tenant>:<scope>:<scopeID>:<alias>:<hex(sha256(input||extraSalt))>
func (c *redisCache) redisKey(k ifaces.Key) string {
	h := sha256.New()
	h.Write(k.NormalizedInput)
	h.Write(k.ExtraSalt)
	digest := hex.EncodeToString(h.Sum(nil))
	return c.keyPrefix + ":" +
		k.TenantID + ":" +
		string(k.Scope) + ":" +
		k.ScopeID + ":" +
		k.Alias + ":" +
		digest
}

// matchPattern returns the Redis glob pattern for an invalidation prefix.
// Tenant is ALWAYS present; invalidation never crosses tenants. Exactly one of
// All/Alias/ScopeID is expected; All wins, and a tenant-only prefix (no
// selector) behaves as All-for-tenant. Patterns (per TASK spec):
//
//	All:     <key_prefix>:<tenant>:*
//	ScopeID: <key_prefix>:<tenant>:*:<scopeID>:*
//	Alias:   <key_prefix>:<tenant>:*:*:<alias>:*
func (c *redisCache) matchPattern(p ifaces.Prefix) string {
	base := c.keyPrefix + ":" + p.TenantID + ":"
	switch {
	case p.All || (p.Alias == "" && p.ScopeID == ""):
		return base + "*"
	case p.Alias != "":
		return base + "*:*:" + p.Alias + ":*"
	default: // p.ScopeID != ""
		return base + "*:" + p.ScopeID + ":*"
	}
}

// statsPattern is the glob for a tenant's full key set (used by Stats).
func (c *redisCache) statsPattern(tenantID string) string {
	return c.keyPrefix + ":" + tenantID + ":*"
}
