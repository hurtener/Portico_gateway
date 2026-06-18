package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// openAIEmbeddingRequest mirrors the OpenAI embeddings request shape.
// Input can be a string or an array of strings.
type openAIEmbeddingRequest struct {
	Model string          `json:"model"`
	Input json.RawMessage `json:"input"` // string or []string
}

// openAIEmbeddingResponse mirrors the OpenAI embeddings response shape.
type openAIEmbeddingResponse struct {
	Object string                `json:"object"`
	Data   []openAIEmbeddingData `json:"data"`
	Model  string                `json:"model"`
	Usage  openAIUsage           `json:"usage"`
}

type openAIEmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// openAIModelsResponse mirrors the OpenAI models list response shape.
type openAIModelsResponse struct {
	Object string               `json:"object"`
	Data   []openAIModelSummary `json:"data"`
}

type openAIModelSummary struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// embeddingsHandler implements POST /v1/embeddings: an OpenAI-compatible
// embeddings surface. The client names a tenant-scoped model alias;
// the gateway resolves it to a provider+model, dispatches through the LLM
// engine, and returns an OpenAI-shaped response.
func embeddingsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMEngine == nil || d.LLMModels == nil || d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "LLM gateway not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !scope.Has(id, ScopeLLMInvoke) && !scope.Has(id, "admin") {
			writeJSONError(w, http.StatusForbidden, "forbidden", "missing required scope "+ScopeLLMInvoke, nil)
			return
		}

		var req openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Model == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "model is required", nil)
			return
		}
		// Phase 14: the agent profile must allow this model alias.
		if !aliasAllowedByProfile(w, r, req.Model) {
			return
		}

		// Parse input: string or []string
		inputStrings, err := parseEmbeddingInput(req.Input)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		if len(inputStrings) == 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "input must contain at least one string", nil)
			return
		}

		// Resolve the alias to a provider + upstream model (tenant-scoped),
		// enforcing model-enabled + the VK provider/model allowlist.
		prov, model, ok := resolveLLMTarget(d, w, r, id.TenantID, req.Model)
		if !ok {
			return
		}

		// Quota: enforce per-tenant limits before dispatch.
		if !checkQuota(d, w, r, id.TenantID, req.Model) {
			return
		}

		resp, err := d.LLMEngine.Embedding(r.Context(), &engineifaces.EmbeddingRequest{
			TenantID:      id.TenantID,
			Provider:      prov.Driver,
			ProviderModel: model.ProviderModel,
			Input:         inputStrings,
		})
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "upstream_error", err.Error(), nil)
			return
		}
		recordQuotaUsage(d, id.TenantID, resp.Usage.TotalTokens)
		// Embeddings have no completion tokens; price on input only.
		recordCost(d, r, id.TenantID, req.Model, prov.Driver, model.ProviderModel, resp.Usage.PromptTokens, 0)

		// Convert to OpenAI shape
		data := make([]openAIEmbeddingData, len(resp.Embeddings))
		for i, emb := range resp.Embeddings {
			data[i] = openAIEmbeddingData{
				Object:    "embedding",
				Index:     i,
				Embedding: emb,
			}
		}
		writeJSON(w, http.StatusOK, openAIEmbeddingResponse{
			Object: "list",
			Data:   data,
			Model:  req.Model, // echo the alias the client requested
			Usage: openAIUsage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: 0,
				TotalTokens:      resp.Usage.TotalTokens,
			},
		})
	}
}

// parseEmbeddingInput accepts a JSON value that is either a string or an
// array of strings and returns a []string slice.
func parseEmbeddingInput(raw json.RawMessage) ([]string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}, nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	return nil, errors.New("input must be a string or array of strings")
}

// listModelsHandler implements GET /v1/models: returns the tenant's enabled
// model aliases in OpenAI-compatible shape.
func listModelsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMModels == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "LLM gateway not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())

		models, err := d.LLMModels.ListModels(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		// Phase 14: only aliases the agent profile allows.
		models = filterAliasesByProfile(r, models, func(m *storageifaces.LLMModel) string { return m.Alias })

		data := make([]openAIModelSummary, 0, len(models))
		for _, m := range models {
			if m.Enabled {
				data = append(data, openAIModelSummary{
					ID:      m.Alias,
					Object:  "model",
					OwnedBy: "portico",
				})
			}
		}

		writeJSON(w, http.StatusOK, openAIModelsResponse{
			Object: "list",
			Data:   data,
		})
	}
}
