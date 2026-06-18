package api

import (
	"errors"
	"net/http"

	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// resolveLLMTarget resolves a model alias to its (provider, model) tenant-scoped
// pair, enforcing model-enabled and the VK provider/model allowlist. On any
// failure it writes the error response and returns ok=false. Shared by the chat
// and embeddings handlers to keep each handler's cyclomatic complexity bounded.
func resolveLLMTarget(d Deps, w http.ResponseWriter, r *http.Request, tenantID, alias string) (*storageifaces.LLMProvider, *storageifaces.LLMModel, bool) {
	model, err := d.LLMModels.GetModel(r.Context(), tenantID, alias)
	if err != nil {
		if errors.Is(err, storageifaces.ErrLLMModelNotFound) {
			writeJSONError(w, http.StatusNotFound, "model_not_found", "unknown model: "+alias, nil)
			return nil, nil, false
		}
		writeJSONError(w, http.StatusInternalServerError, "resolve_failed", err.Error(), nil)
		return nil, nil, false
	}
	if !model.Enabled {
		writeJSONError(w, http.StatusForbidden, "model_disabled", "model is disabled: "+alias, nil)
		return nil, nil, false
	}
	prov, err := d.LLMProviders.GetProvider(r.Context(), tenantID, model.ProviderName)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "provider_missing", "provider not found for alias", nil)
		return nil, nil, false
	}
	if !vkAllowsLLM(w, r, alias, prov.Driver) {
		return nil, nil, false
	}
	return prov, model, true
}

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
