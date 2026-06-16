// Package bifrost adapts the embedded Bifrost Go SDK
// (github.com/maximhq/bifrost/core) to Portico's engine seam. It is the ONLY
// package that imports the Bifrost module (CLAUDE.md §4.4 / §13). Bifrost is used
// as a library — never the bifrost-http sidecar.
package bifrost

import (
	"context"
	"errors"
	"fmt"
	"sync"

	bcore "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
)

const driverName = "bifrost"

// nativeProviders are Bifrost's built-in provider driver names (v1.4.0).
var nativeProviders = []string{
	"openai", "azure", "anthropic", "bedrock", "cohere", "vertex", "mistral",
	"ollama", "groq", "sgl", "parasail", "perplexity", "cerebras", "gemini",
	"openrouter", "elevenlabs", "huggingface", "nebius", "xai",
}

// adapter implements ifaces.Engine by dispatching to per-tenant Bifrost clients.
// A client is created lazily per tenant (the Bifrost Account interface is not
// tenant-parameterised, so tenant isolation lives in the bound porticoAccount).
type adapter struct {
	deps    ifaces.Deps
	mu      sync.Mutex
	clients map[string]*bcore.Bifrost
}

func newAdapter(deps ifaces.Deps) *adapter {
	return &adapter{deps: deps, clients: make(map[string]*bcore.Bifrost)}
}

func (a *adapter) Name() string { return driverName }

// ProvidersSupported declares the driver names this engine can route to: Bifrost's
// natives plus Portico's custom_openai slot.
func (a *adapter) ProvidersSupported() []string {
	out := make([]string, 0, len(nativeProviders)+1)
	out = append(out, nativeProviders...)
	out = append(out, "custom_openai")
	return out
}

func (a *adapter) clientFor(ctx context.Context, tenantID string) (*bcore.Bifrost, error) {
	if tenantID == "" {
		return nil, errors.New("bifrost: empty tenant id")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.clients[tenantID]; ok {
		return c, nil
	}
	acct := &porticoAccount{deps: a.deps, tenantID: tenantID}
	c, err := bcore.Init(ctx, schemas.BifrostConfig{Account: acct})
	if err != nil {
		return nil, fmt.Errorf("bifrost: init client for tenant %q: %w", tenantID, err)
	}
	a.clients[tenantID] = c
	return c, nil
}

// ChatCompletion dispatches a non-streaming chat completion.
func (a *adapter) ChatCompletion(ctx context.Context, req *ifaces.ChatRequest) (*ifaces.ChatResponse, error) {
	client, err := a.clientFor(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	bctx, cancel := schemas.NewBifrostContextWithCancel(ctx)
	defer cancel()
	resp, bErr := client.ChatCompletionRequest(bctx, &schemas.BifrostChatRequest{
		Provider: schemas.ModelProvider(req.Provider),
		Model:    req.ProviderModel,
		Input:    toBifrostMessages(req.Messages),
		Params:   toChatParams(req),
	})
	if bErr != nil {
		return nil, bifrostError(bErr)
	}
	return fromBifrostChatResponse(req.Provider, resp), nil
}

// ChatCompletionStream dispatches a streaming chat completion, forwarding deltas
// on the returned channel and a terminal Done (or Err) chunk.
func (a *adapter) ChatCompletionStream(ctx context.Context, req *ifaces.ChatRequest) (<-chan ifaces.ChatChunk, error) {
	client, err := a.clientFor(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	bctx, cancel := schemas.NewBifrostContextWithCancel(ctx)
	stream, bErr := client.ChatCompletionStreamRequest(bctx, &schemas.BifrostChatRequest{
		Provider: schemas.ModelProvider(req.Provider),
		Model:    req.ProviderModel,
		Input:    toBifrostMessages(req.Messages),
		Params:   toChatParams(req),
	})
	if bErr != nil {
		cancel()
		return nil, bifrostError(bErr)
	}
	out := make(chan ifaces.ChatChunk)
	go func() {
		defer cancel()
		defer close(out)
		for chunk := range stream {
			if chunk.BifrostError != nil {
				out <- ifaces.ChatChunk{Err: bifrostError(chunk.BifrostError)}
				return
			}
			if d := streamDelta(chunk); d != "" {
				out <- ifaces.ChatChunk{Delta: d}
			}
		}
		out <- ifaces.ChatChunk{Done: true}
	}()
	return out, nil
}

// Embedding dispatches an embedding request.
func (a *adapter) Embedding(ctx context.Context, req *ifaces.EmbeddingRequest) (*ifaces.EmbeddingResponse, error) {
	client, err := a.clientFor(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	bctx, cancel := schemas.NewBifrostContextWithCancel(ctx)
	defer cancel()
	input := req.Input
	resp, bErr := client.EmbeddingRequest(bctx, &schemas.BifrostEmbeddingRequest{
		Provider: schemas.ModelProvider(req.Provider),
		Model:    req.ProviderModel,
		Input:    &schemas.EmbeddingInput{Texts: input},
	})
	if bErr != nil {
		return nil, bifrostError(bErr)
	}
	return fromBifrostEmbeddingResponse(req.Provider, resp), nil
}

// Health reports the engine's view of the tenant-agnostic provider set. A real
// per-provider probe is added when the /llm/health surface lands; for now it
// reports the supported drivers as reachable=true.
func (a *adapter) Health(_ context.Context) ([]ifaces.ProviderHealth, error) {
	out := make([]ifaces.ProviderHealth, 0, len(nativeProviders))
	for _, d := range nativeProviders {
		out = append(out, ifaces.ProviderHealth{Provider: d, Driver: d, Healthy: true, Detail: "configured"})
	}
	return out, nil
}

// ---- type mapping (Portico ifaces <-> Bifrost schemas) ----

func toBifrostMessages(msgs []ifaces.ChatMessage) []schemas.ChatMessage {
	out := make([]schemas.ChatMessage, 0, len(msgs))
	for i := range msgs {
		content := msgs[i].Content
		out = append(out, schemas.ChatMessage{
			Role:    schemas.ChatMessageRole(msgs[i].Role),
			Content: &schemas.ChatMessageContent{ContentStr: &content},
		})
	}
	return out
}

func toChatParams(req *ifaces.ChatRequest) *schemas.ChatParameters {
	if req.Temperature == nil && req.MaxTokens == nil {
		return nil
	}
	return &schemas.ChatParameters{
		Temperature:         req.Temperature,
		MaxCompletionTokens: req.MaxTokens,
	}
}

func fromBifrostChatResponse(provider string, resp *schemas.BifrostChatResponse) *ifaces.ChatResponse {
	out := &ifaces.ChatResponse{Provider: provider}
	if resp == nil {
		return out
	}
	out.ID = resp.ID
	out.Model = resp.Model
	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		if ch.ChatNonStreamResponseChoice != nil && ch.ChatNonStreamResponseChoice.Message != nil {
			msg := ch.ChatNonStreamResponseChoice.Message
			out.Message.Role = string(msg.Role)
			if msg.Content != nil && msg.Content.ContentStr != nil {
				out.Message.Content = *msg.Content.ContentStr
			}
		}
	}
	if resp.Usage != nil {
		out.Usage = ifaces.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return out
}

func fromBifrostEmbeddingResponse(provider string, resp *schemas.BifrostEmbeddingResponse) *ifaces.EmbeddingResponse {
	out := &ifaces.EmbeddingResponse{Provider: provider}
	if resp == nil {
		return out
	}
	out.Model = resp.Model
	out.Embeddings = make([][]float64, 0, len(resp.Data))
	for i := range resp.Data {
		out.Embeddings = append(out.Embeddings, embeddingVector(resp.Data[i]))
	}
	if resp.Usage != nil {
		out.Usage = ifaces.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return out
}

// embeddingVector flattens a Bifrost embedding into a []float64.
func embeddingVector(d schemas.EmbeddingData) []float64 {
	arr := d.Embedding.EmbeddingArray
	if arr == nil && len(d.Embedding.Embedding2DArray) > 0 {
		arr = d.Embedding.Embedding2DArray[0]
	}
	out := make([]float64, len(arr))
	for i, v := range arr {
		out[i] = float64(v)
	}
	return out
}

// streamDelta extracts the incremental text from a Bifrost stream chunk.
func streamDelta(chunk *schemas.BifrostStreamChunk) string {
	if chunk == nil || chunk.BifrostChatResponse == nil {
		return ""
	}
	for _, ch := range chunk.BifrostChatResponse.Choices {
		if ch.ChatStreamResponseChoice != nil && ch.ChatStreamResponseChoice.Delta != nil {
			if d := ch.ChatStreamResponseChoice.Delta.Content; d != nil {
				return *d
			}
		}
	}
	return ""
}

func bifrostError(e *schemas.BifrostError) error {
	if e == nil {
		return nil
	}
	msg := "unknown upstream error"
	if e.Error != nil {
		switch {
		case e.Error.Message != "":
			msg = e.Error.Message
		case e.Error.Error != nil:
			msg = e.Error.Error.Error()
		}
		if e.Error.Type != nil && *e.Error.Type != "" {
			msg = *e.Error.Type + ": " + msg
		}
	} else if e.Type != nil && *e.Type != "" {
		msg = *e.Type
	}
	if e.StatusCode != nil {
		return fmt.Errorf("bifrost: upstream status %d: %s", *e.StatusCode, msg)
	}
	return fmt.Errorf("bifrost: %s", msg)
}
