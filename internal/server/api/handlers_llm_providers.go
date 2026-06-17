package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// requireLLMAdmin gates LLM write routes behind the admin umbrella scope.
func requireLLMAdmin(w http.ResponseWriter, id tenant.Identity) bool {
	if scope.Has(id, "admin") {
		return true
	}
	writeJSONError(w, http.StatusForbidden, "forbidden", "admin scope required", nil)
	return false
}

// --- provider DTOs ---

type llmProviderDTO struct {
	Name          string         `json:"name"`
	Driver        string         `json:"driver"`
	Config        map[string]any `json:"config,omitempty"`
	CredentialRef string         `json:"credential_ref,omitempty"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     string         `json:"created_at,omitempty"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
}

func providerToDTO(p *ifaces.LLMProvider) llmProviderDTO {
	dto := llmProviderDTO{
		Name: p.Name, Driver: p.Driver, CredentialRef: p.CredentialRef,
		Enabled: p.Enabled, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if p.ConfigJSON != "" && p.ConfigJSON != "{}" {
		_ = json.Unmarshal([]byte(p.ConfigJSON), &dto.Config)
	}
	return dto
}

func (dto llmProviderDTO) toProvider(tenantID, name string) (*ifaces.LLMProvider, error) {
	cfg := "{}"
	if len(dto.Config) > 0 {
		b, err := json.Marshal(dto.Config)
		if err != nil {
			return nil, err
		}
		cfg = string(b)
	}
	return &ifaces.LLMProvider{
		TenantID: tenantID, Name: name, Driver: dto.Driver, ConfigJSON: cfg,
		CredentialRef: dto.CredentialRef, Enabled: dto.Enabled,
	}, nil
}

// listLLMProvidersHandler GET /api/llm/providers.
func listLLMProvidersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		provs, err := d.LLMProviders.ListProviders(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]llmProviderDTO, 0, len(provs))
		for _, p := range provs {
			out = append(out, providerToDTO(p))
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": out})
	}
}

// getLLMProviderHandler GET /api/llm/providers/{name}.
func getLLMProviderHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		p, err := d.LLMProviders.GetProvider(r.Context(), id.TenantID, chi.URLParam(r, "name"))
		if err != nil {
			if errors.Is(err, ifaces.ErrLLMProviderNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "provider not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, providerToDTO(p))
	}
}

// upsertLLMProviderHandler handles POST (create) and PUT (update).
func upsertLLMProviderHandler(d Deps, update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		var dto llmProviderDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		name := dto.Name
		if update {
			name = chi.URLParam(r, "name")
		}
		if name == "" || dto.Driver == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "name and driver are required", nil)
			return
		}
		p, err := dto.toProvider(id.TenantID, name)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_config", err.Error(), nil)
			return
		}
		if update {
			if err := d.LLMProviders.UpdateProvider(r.Context(), p); err != nil {
				if errors.Is(err, ifaces.ErrLLMProviderNotFound) {
					writeJSONError(w, http.StatusNotFound, "not_found", "provider not found", nil)
					return
				}
				writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
				return
			}
			writeJSON(w, http.StatusOK, providerToDTO(p))
			return
		}
		if err := d.LLMProviders.CreateProvider(r.Context(), p); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, providerToDTO(p))
	}
}

// deleteLLMProviderHandler DELETE /api/llm/providers/{name}.
func deleteLLMProviderHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		err := d.LLMProviders.DeleteProvider(r.Context(), id.TenantID, chi.URLParam(r, "name"))
		if err != nil {
			if errors.Is(err, ifaces.ErrLLMProviderNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "provider not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- provider key DTOs + handlers ---

type llmProviderKeyDTO struct {
	KeyID          string   `json:"key_id,omitempty"`
	CredentialRef  string   `json:"credential_ref"`
	Weight         float64  `json:"weight,omitempty"`
	ModelAllowlist []string `json:"model_allowlist,omitempty"`
	Enabled        bool     `json:"enabled"`
	CreatedAt      string   `json:"created_at,omitempty"`
}

func keyToDTO(k *ifaces.LLMProviderKey) llmProviderKeyDTO {
	dto := llmProviderKeyDTO{
		KeyID: k.KeyID, CredentialRef: k.CredentialRef, Weight: k.Weight,
		Enabled: k.Enabled, CreatedAt: k.CreatedAt,
	}
	if k.ModelAllowlist != "" && k.ModelAllowlist != "[]" {
		_ = json.Unmarshal([]byte(k.ModelAllowlist), &dto.ModelAllowlist)
	}
	return dto
}

// listLLMProviderKeysHandler GET /api/llm/providers/{name}/keys.
func listLLMProviderKeysHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		keys, err := d.LLMProviders.ListKeys(r.Context(), id.TenantID, chi.URLParam(r, "name"))
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]llmProviderKeyDTO, 0, len(keys))
		for _, k := range keys {
			out = append(out, keyToDTO(k))
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": out})
	}
}

// addLLMProviderKeyHandler POST /api/llm/providers/{name}/keys.
func addLLMProviderKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		var dto llmProviderKeyDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if dto.CredentialRef == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "credential_ref is required", nil)
			return
		}
		keyID := dto.KeyID
		if keyID == "" {
			keyID = ulid.Make().String()
		}
		allow := "[]"
		if len(dto.ModelAllowlist) > 0 {
			b, _ := json.Marshal(dto.ModelAllowlist)
			allow = string(b)
		}
		k := &ifaces.LLMProviderKey{
			TenantID: id.TenantID, ProviderName: chi.URLParam(r, "name"), KeyID: keyID,
			CredentialRef: dto.CredentialRef, Weight: dto.Weight, ModelAllowlist: allow, Enabled: dto.Enabled,
		}
		if err := d.LLMProviders.AddKey(r.Context(), k); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "add_key_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, keyToDTO(k))
	}
}

// deleteLLMProviderKeyHandler DELETE /api/llm/providers/{name}/keys/{keyID}.
func deleteLLMProviderKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm provider store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		err := d.LLMProviders.DeleteKey(r.Context(), id.TenantID, chi.URLParam(r, "name"), chi.URLParam(r, "keyID"))
		if err != nil {
			if errors.Is(err, ifaces.ErrLLMProviderNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "key not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_key_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
