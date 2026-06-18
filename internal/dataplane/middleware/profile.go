// Package middleware holds the Phase 14 Agent Profile request middleware — the
// single step that sits after tenant + JWT/VK resolution and before policy, and
// writes the resolved *profiles.Profile into the request context. It is the only
// file under internal/dataplane/middleware/; the rest of the dataplane substrate
// (listeners/routes/backends) was deliberately not built (the 2026-05-12 pivot).
package middleware

import (
	"log/slog"
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

// ProfileMiddleware resolves the request principal's agent profile and writes it
// into the context for every downstream gating surface (MCP dispatcher, LLM
// handler, Skills runtime) to read via profiles.FromContext.
//
// It is additive and fail-safe at the edges: a request with no identity (a
// public path) passes through untouched. It fails CLOSED on a resolver/store
// error (503), since a request whose entitlement can't be determined must not
// proceed with an assumed-full surface. A principal with no profile bound
// resolves to the default profile (full tenant surface) — the back-compat seam,
// not an error.
func ProfileMiddleware(resolver profiles.Resolver, log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := tenant.From(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			subject := id.Subject
			if subject == "" {
				subject = id.UserID
			}
			prof, err := resolver.Resolve(r.Context(), profiles.Principal{
				TenantID: id.TenantID,
				Subject:  subject,
			})
			if err != nil {
				log.Warn("agent profile resolution failed; failing closed",
					"tenant_id", id.TenantID, "err", err)
				http.Error(w, `{"error":"profile_unavailable","message":"could not resolve agent profile"}`, http.StatusServiceUnavailable)
				return
			}
			ctx := profiles.WithProfile(r.Context(), prof)

			// Scope intersection (Phase 14, acceptance #11): a non-default profile
			// that carries its own scope set narrows the principal's effective
			// scopes to (profile.Scopes ∩ identity.Scopes). The profile may
			// restrict but never broaden — a scope the JWT did not carry is never
			// granted. An empty profile.Scopes means "the profile does not constrain
			// scopes", so the JWT set passes through unchanged.
			if !prof.IsDefault && len(prof.Scopes) > 0 {
				id.Scopes = intersectScopes(id.Scopes, prof.Scopes)
				ctx = tenant.With(ctx, id)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// intersectScopes returns the scopes present in BOTH have and allow, preserving
// the order of have (the principal's JWT scopes). Most-restrictive wins.
func intersectScopes(have, allow []string) []string {
	allowed := make(map[string]struct{}, len(allow))
	for _, s := range allow {
		allowed[s] = struct{}{}
	}
	out := make([]string, 0, len(have))
	for _, s := range have {
		if _, ok := allowed[s]; ok {
			out = append(out, s)
		}
	}
	return out
}
