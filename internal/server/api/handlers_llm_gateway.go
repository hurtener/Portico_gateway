package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ScopeLLMInvoke is required to call the OpenAI-compatible chat surface.
const ScopeLLMInvoke = "llm:invoke"

// --- OpenAI-compatible wire types (northbound) ---

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// chatCompletionsHandler implements POST /v1/chat/completions: an
// OpenAI-compatible chat surface. The client names a tenant-scoped model alias;
// the gateway resolves it to a provider+model, dispatches through the LLM engine,
// and returns an OpenAI-shaped response. Streaming is a later unit.
func chatCompletionsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMEngine == nil || d.LLMModels == nil || d.LLMProviders == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "LLM gateway not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		// admin is the umbrella scope (matches scope.Require's hasScopeOrAdmin).
		if !scope.Has(id, ScopeLLMInvoke) && !scope.Has(id, "admin") {
			writeJSONError(w, http.StatusForbidden, "forbidden", "missing required scope "+ScopeLLMInvoke, nil)
			return
		}

		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Model == "" || len(req.Messages) == 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "model and messages are required", nil)
			return
		}
		// Resolve the alias to a provider + upstream model (tenant-scoped) for BOTH stream and non-stream paths.
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

		// Quota: enforce per-tenant limits before dispatch (both stream + non-stream).
		if !checkQuota(d, w, r, id.TenantID, req.Model) {
			return
		}

		if req.Stream {
			streamChatCompletion(w, r, d, id.TenantID, req.Model, prov, model, req)
			return
		}

		resp, err := d.LLMEngine.ChatCompletion(r.Context(), &engineifaces.ChatRequest{
			TenantID:      id.TenantID,
			Provider:      prov.Driver,
			ProviderModel: model.ProviderModel,
			Messages:      toEngineMessages(req.Messages),
			Temperature:   req.Temperature,
			MaxTokens:     req.MaxTokens,
		})
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "upstream_error", err.Error(), nil)
			return
		}
		recordQuotaUsage(d, id.TenantID, resp.Usage.TotalTokens)
		recordCost(d, r, id.TenantID, req.Model, prov.Driver, model.ProviderModel,
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
		recordChatSession(d, r, req.Model, req.Messages,
			openAIMessage{Role: orDefault(resp.Message.Role, "assistant"), Content: resp.Message.Content})

		writeJSON(w, http.StatusOK, openAIChatResponse{
			ID:      orDefault(resp.ID, "chatcmpl-portico"),
			Object:  "chat.completion",
			Created: time.Now().UTC().Unix(),
			Model:   req.Model, // echo the alias the client requested
			Choices: []openAIChoice{{
				Index:        0,
				Message:      openAIMessage{Role: orDefault(resp.Message.Role, "assistant"), Content: resp.Message.Content},
				FinishReason: "stop",
			}},
			Usage: openAIUsage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
			},
		})
	}
}

func toEngineMessages(msgs []openAIMessage) []engineifaces.ChatMessage {
	out := make([]engineifaces.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, engineifaces.ChatMessage{Role: m.Role, Content: m.Content})
	}
	return out
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
