// Package scope is a thin helper around Identity.HasScope that adds
// HTTP-middleware sugar.
package scope

import (
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// Phase 9 introduces named scope constants so the API + Console reference
// the same vocabulary. The Console mirrors these strings in
// web/console/src/lib/scopes.ts — keep both in sync. Drift between the
// two surfaces produces confusing UX (per docs/plans/phase-9 common
// pitfalls).
const (
	// ScopeAdmin is the existing umbrella admin scope. Kept for back-compat.
	ScopeAdmin = "admin"

	// ScopeServersWrite — required to register / edit / delete MCP servers.
	ScopeServersWrite = "servers:write"

	// ScopeSecretsWrite — required for vault CRUD + reveal + rotate.
	//nolint:gosec // scope label, not a credential
	ScopeSecretsWrite = "secrets:write"

	// ScopePolicyWrite — required to edit policy rules.
	ScopePolicyWrite = "policy:write"

	// ScopeTenantsAdmin — required to create / archive / purge tenants.
	ScopeTenantsAdmin = "tenants:admin"
)

// Has reports whether the identity has the named scope.
func Has(id tenant.Identity, s string) bool {
	return id.HasScope(s)
}

// Require returns middleware that rejects requests whose identity lacks the
// named scope. Must be mounted after the tenant auth middleware.
//
// The admin scope acts as a wildcard — operators with `admin` are
// implicitly granted every named write scope. Per-resource scopes
// (servers:write, secrets:write, …) are also accepted directly.
func Require(s string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := tenant.From(r.Context())
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized","message":"missing identity"}`))
				return
			}
			if !hasScopeOrAdmin(id, s) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"permission_denied","message":"missing scope ` + s + `"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hasScopeOrAdmin returns true if the identity carries the named scope or
// the umbrella admin scope. Phase 9 introduced named per-resource scopes;
// admin remains the implicit superset to keep dev mode + existing
// deployments working.
func hasScopeOrAdmin(id tenant.Identity, s string) bool {
	if s == "" {
		return true
	}
	if id.HasScope(s) {
		return true
	}
	if s != ScopeAdmin && id.HasScope(ScopeAdmin) {
		return true
	}
	return false
}
