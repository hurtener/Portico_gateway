// Package ifaces defines the LLM inference-engine seam: the Engine interface a
// driver implements, the Driver factory contract, and the value types that cross
// the boundary. Concrete engines (e.g. the Bifrost adapter) live one level down
// and self-register via internal/llm/engine.Register.
package ifaces

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hurtener/Portico_gateway/internal/secrets"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ErrUpstreamUnavailable is returned by an Engine when the upstream inference
// provider is unreachable, so callers can distinguish it from a request error.
var ErrUpstreamUnavailable = errors.New("upstream inference engine unavailable")

// ChatMessage is one message in a chat completion exchange.
type ChatMessage struct {
	Role    string // "system" | "user" | "assistant" | "tool"
	Content string
}

// ChatRequest is a fully-resolved chat completion request: the provider and the
// upstream model id have already been chosen by the registry/alias resolver.
type ChatRequest struct {
	TenantID      string
	Provider      string // resolved provider name
	ProviderModel string // resolved upstream model id (e.g. "gpt-4o")
	Messages      []ChatMessage
	Temperature   *float64 // nil = engine/provider default
	MaxTokens     *int     // nil = engine/provider default
	Stream        bool
}

// Usage carries token accounting for a single call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ChatResponse is a non-streaming chat completion result.
type ChatResponse struct {
	ID       string
	Provider string
	Model    string
	Message  ChatMessage
	Usage    Usage
}

// ChatChunk is one streamed delta. Done marks the final chunk; Err carries a
// mid-stream failure (terminal for the stream).
type ChatChunk struct {
	Delta string
	Done  bool
	Err   error
}

// EmbeddingRequest is a fully-resolved embedding request.
type EmbeddingRequest struct {
	TenantID      string
	Provider      string
	ProviderModel string
	Input         []string
}

// EmbeddingResponse carries one embedding vector per input.
type EmbeddingResponse struct {
	Provider   string
	Model      string
	Embeddings [][]float64
	Usage      Usage
}

// ProviderHealth is the engine's view of one provider it has been asked to use.
type ProviderHealth struct {
	Provider string
	Driver   string
	Healthy  bool
	Detail   string
}

// Deps carries the cross-cutting services an engine driver may need. The driver
// reads provider configs from Providers (tenant-scoped) and resolves their API
// keys from Vault on every dispatch (no plaintext caching). More services
// (audit, tracer) are added when a unit consumes them.
type Deps struct {
	Logger    *slog.Logger
	Providers storageifaces.LLMProviderStore
	Vault     secrets.Vault
}

// Engine dispatches fully-resolved requests to an underlying inference engine.
type Engine interface {
	Name() string
	ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
	Embedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
	// ProvidersSupported declares the driver names this engine can route to, so the
	// provider registry can reject an unservable driver at config-load time.
	ProvidersSupported() []string
	Health(ctx context.Context) ([]ProviderHealth, error)
}

// Driver builds an Engine. Drivers self-register from their init() via
// internal/llm/engine.Register and are pulled in by a blank import in cmd/portico.
type Driver interface {
	Name() string
	New(cfg map[string]any, deps Deps) (Engine, error)
}
