package api

import (
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/profiles"
)

// Agent Profile enforcement on the LLM path (Phase 14, acceptance #7). The
// profile is resolved into the request context by the profile middleware; the
// LLM handlers gate the requested model alias against AllowsAlias. A nil or
// default profile allows everything (back-compat).

// aliasAllowedByProfile reports whether the request's profile permits the model
// alias. When it does not, it writes a 403 agent_profile_violation and returns
// false (the caller returns immediately).
func aliasAllowedByProfile(w http.ResponseWriter, r *http.Request, alias string) bool {
	prof := profiles.FromContext(r.Context())
	if prof == nil || prof.IsDefault || prof.AllowsAlias(alias) {
		return true
	}
	writeJSONError(w, http.StatusForbidden, "agent_profile_violation",
		"model alias is outside the agent profile surface: "+alias,
		map[string]any{"profile_id": prof.ID, "alias": alias})
	return false
}

// filterAliasesByProfile drops items whose model alias the profile disallows
// (for GET /v1/models). A nil/default profile returns the slice unchanged.
func filterAliasesByProfile[T any](r *http.Request, items []T, aliasOf func(T) string) []T {
	prof := profiles.FromContext(r.Context())
	if prof == nil || prof.IsDefault {
		return items
	}
	out := make([]T, 0, len(items))
	for _, it := range items {
		if prof.AllowsAlias(aliasOf(it)) {
			out = append(out, it)
		}
	}
	return out
}
