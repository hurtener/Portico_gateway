// Package tenant carries tenant + user identity through request context.
//
// Identity is populated by the JWT validation middleware (or by the dev-mode
// bypass) and read by every handler that touches tenant-scoped data.
package tenant

import (
	"context"
	"errors"
)

// Identity describes the principal making a request.
type Identity struct {
	TenantID string
	UserID   string
	Plan     string
	Scopes   []string
	Issuer   string
	Subject  string
	DevMode  bool
}

// HasScope reports whether the identity carries the given scope.
func (i Identity) HasScope(s string) bool {
	for _, x := range i.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

type ctxKey struct{}

// With returns a child context carrying the identity.
func With(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From extracts the identity. Second return is false if missing.
func From(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(ctxKey{}).(Identity)
	return v, ok
}

// MustFrom panics if no identity is present. Use only in handlers downstream
// of the auth middleware where identity presence is an invariant.
func MustFrom(ctx context.Context) Identity {
	id, ok := From(ctx)
	if !ok {
		panic(errors.New("tenant: identity missing from context (handler reached without auth middleware?)"))
	}
	return id
}
