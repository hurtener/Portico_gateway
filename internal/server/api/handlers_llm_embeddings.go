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

		// Resolve the alias to a provider + upstream model (tenant-scoped).
		model, err := d.LLMModels.GetModel(r.Context(), id.TenantID, req.Model)
		if err != nil {
			if errors.Is(err, storageifaces.ErrLLMModelNotFound) {
				writeJSONError(w, http.StatusNotFound, "model_not_found", "unknown model: "+req.Model, nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "resolve_failed", err.Error(), nil)
			return
		}
		if !model.Enabled {
			writeJSONError(w, http.StatusForbidden, "model_disabled", "model is disabled: "+req.Model, nil)
			return
		}
		prov, err := d.LLMProviders.GetProvider(r.Context(), id.TenantID, model.ProviderName)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "provider_missing", "provider not found for alias", nil)
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
