package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// --- model alias DTOs ---

type llmModelDTO struct {
	Alias         string         `json:"alias"`
	ProviderName  string         `json:"provider_name"`
	ProviderModel string         `json:"provider_model"`
	DefaultParams map[string]any `json:"default_params,omitempty"`
	Capabilities  []string       `json:"capabilities,omitempty"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     string         `json:"created_at,omitempty"`
	UpdatedAt     string         `json:"updated_at,omitempty"`
}

func modelToDTO(m *ifaces.LLMModel) llmModelDTO {
	dto := llmModelDTO{
		Alias: m.Alias, ProviderName: m.ProviderName, ProviderModel: m.ProviderModel,
		Enabled: m.Enabled, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.DefaultParamsJSON != "" && m.DefaultParamsJSON != "{}" {
		_ = json.Unmarshal([]byte(m.DefaultParamsJSON), &dto.DefaultParams)
	}
	if m.Capabilities != "" && m.Capabilities != "[]" {
		_ = json.Unmarshal([]byte(m.Capabilities), &dto.Capabilities)
	}
	return dto
}

func (dto llmModelDTO) toModel(tenantID, alias string) (*ifaces.LLMModel, error) {
	params := "{}"
	if len(dto.DefaultParams) > 0 {
		b, err := json.Marshal(dto.DefaultParams)
		if err != nil {
			return nil, err
		}
		params = string(b)
	}
	caps := "[]"
	if len(dto.Capabilities) > 0 {
		b, err := json.Marshal(dto.Capabilities)
		if err != nil {
			return nil, err
		}
		caps = string(b)
	}
	return &ifaces.LLMModel{
		TenantID: tenantID, Alias: alias, ProviderName: dto.ProviderName,
		ProviderModel: dto.ProviderModel, DefaultParamsJSON: params, Capabilities: caps, Enabled: dto.Enabled,
	}, nil
}

// listLLMModelsHandler GET /api/llm/models.
func listLLMModelsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMModels == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm model store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		models, err := d.LLMModels.ListModels(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]llmModelDTO, 0, len(models))
		for _, m := range models {
			out = append(out, modelToDTO(m))
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": out})
	}
}

// getLLMModelHandler GET /api/llm/models/{alias}.
func getLLMModelHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMModels == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm model store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		m, err := d.LLMModels.GetModel(r.Context(), id.TenantID, chi.URLParam(r, "alias"))
		if err != nil {
			if errors.Is(err, ifaces.ErrLLMModelNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "model not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, modelToDTO(m))
	}
}

// upsertLLMModelHandler handles POST (create) and PUT (update).
func upsertLLMModelHandler(d Deps, update bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMModels == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm model store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		var dto llmModelDTO
		if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		alias := dto.Alias
		if update {
			alias = chi.URLParam(r, "alias")
		}
		if alias == "" || dto.ProviderName == "" || dto.ProviderModel == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "alias, provider_name and provider_model are required", nil)
			return
		}
		m, err := dto.toModel(id.TenantID, alias)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_params", err.Error(), nil)
			return
		}
		if update {
			if err := d.LLMModels.UpdateModel(r.Context(), m); err != nil {
				if errors.Is(err, ifaces.ErrLLMModelNotFound) {
					writeJSONError(w, http.StatusNotFound, "not_found", "model not found", nil)
					return
				}
				writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
				return
			}
			writeJSON(w, http.StatusOK, modelToDTO(m))
			return
		}
		if err := d.LLMModels.CreateModel(r.Context(), m); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, modelToDTO(m))
	}
}

// deleteLLMModelHandler DELETE /api/llm/models/{alias}.
func deleteLLMModelHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMModels == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm model store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}
		err := d.LLMModels.DeleteModel(r.Context(), id.TenantID, chi.URLParam(r, "alias"))
		if err != nil {
			if errors.Is(err, ifaces.ErrLLMModelNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "model not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
