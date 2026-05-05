// Package scope is a thin helper around Identity.HasScope that adds
// HTTP-middleware sugar.
package scope

import (
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
)

// Has reports whether the identity has the named scope.
func Has(id tenant.Identity, s string) bool {
	return id.HasScope(s)
}

// Require returns middleware that rejects requests whose identity lacks the
// named scope. Must be mounted after the tenant auth middleware.
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
			if !id.HasScope(s) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden","message":"missing scope ` + s + `"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
