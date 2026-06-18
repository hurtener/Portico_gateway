package api

import (
	"net/http"

	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
)

// vkAllowsLLM enforces a Virtual Key's provider + model allowlists on an LLM
// request. When the caller is not a VK (e.g. JWT auth), it is a no-op (true).
// A violation writes 403 vk_scope_violation and returns false. Empty allowlists
// mean "all" (the VK does not narrow that dimension). This layers on top of the
// Agent Profile checks (VK ∩ Profile, most-restrictive-wins): both must pass.
func vkAllowsLLM(w http.ResponseWriter, r *http.Request, alias, providerDriver string) bool {
	res, ok := virtualkeys.FromContext(r.Context())
	if !ok {
		return true
	}
	if !res.AllowsModel(alias) {
		writeJSONError(w, http.StatusForbidden, "vk_scope_violation",
			"model alias is outside the virtual key's allowlist: "+alias,
			map[string]any{"vk_id": res.VKID, "alias": alias})
		return false
	}
	if !res.AllowsProvider(providerDriver) {
		writeJSONError(w, http.StatusForbidden, "vk_scope_violation",
			"provider is outside the virtual key's allowlist: "+providerDriver,
			map[string]any{"vk_id": res.VKID, "provider": providerDriver})
		return false
	}
	return true
}
