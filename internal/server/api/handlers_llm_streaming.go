package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// openAIChatChunk is the OpenAI chat.completion.chunk SSE payload.
type openAIChatChunk struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []openAIChunkChoice `json:"choices"`
}

type openAIChunkChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type openAIDelta struct {
	Content string `json:"content,omitempty"`
	Role    string `json:"role,omitempty"`
}

// streamChatCompletion handles Server-Sent Events streaming for chat completions.
func streamChatCompletion(w http.ResponseWriter, r *http.Request, d Deps, tenantID, modelAlias string, prov *storageifaces.LLMProvider, model *storageifaces.LLMModel, req openAIChatRequest) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming not supported by response writer", nil)
		return
	}

	ctx := r.Context()
	chunkCh, err := d.LLMEngine.ChatCompletionStream(ctx, &engineifaces.ChatRequest{
		TenantID:      tenantID,
		Provider:      prov.Driver,
		ProviderModel: model.ProviderModel,
		Messages:      toEngineMessages(req.Messages),
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
	})
	if err != nil {
		writeSSEError(w, flusher, "upstream_error", err.Error())
		return
	}

	chunkID := fmt.Sprintf("chatcmpl-%s", time.Now().UTC().Format("20060102150405"))
	created := time.Now().UTC().Unix()

	for chunk := range chunkCh {
		if chunk.Err != nil {
			writeSSEError(w, flusher, "stream_error", chunk.Err.Error())
			return
		}

		if chunk.Delta != "" {
			deltaChunk := openAIChatChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   modelAlias,
				Choices: []openAIChunkChoice{{
					Index:        0,
					Delta:        openAIDelta{Content: chunk.Delta},
					FinishReason: nil,
				}},
			}
			writeSSEData(w, flusher, deltaChunk)
		}

		if chunk.Done {
			// Send final chunk with finish_reason: "stop"
			finalChunk := openAIChatChunk{
				ID:      chunkID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   modelAlias,
				Choices: []openAIChunkChoice{{
					Index:        0,
					Delta:        openAIDelta{},
					FinishReason: strPtr("stop"),
				}},
			}
			writeSSEData(w, flusher, finalChunk)
			// Terminal [DONE] marker
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}

	// If channel closed without Done, send final chunk and [DONE]
	finalChunk := openAIChatChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   modelAlias,
		Choices: []openAIChunkChoice{{
			Index:        0,
			Delta:        openAIDelta{},
			FinishReason: strPtr("stop"),
		}},
	}
	writeSSEData(w, flusher, finalChunk)
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeSSEData(w http.ResponseWriter, flusher http.Flusher, data any) {
	payload, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, code, message string) {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func strPtr(s string) *string {
	return &s
}
