package engine

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
)

type fakeEngine struct {
	name string
}

func (f *fakeEngine) Name() string {
	return f.name
}

func (f *fakeEngine) ChatCompletion(ctx context.Context, req *ifaces.ChatRequest) (*ifaces.ChatResponse, error) {
	return &ifaces.ChatResponse{
		ID:       "test-id",
		Provider: req.Provider,
		Model:    req.ProviderModel,
		Message:  ifaces.ChatMessage{Role: "assistant", Content: "canned response"},
		Usage:    ifaces.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}, nil
}

func (f *fakeEngine) ChatCompletionStream(ctx context.Context, req *ifaces.ChatRequest) (<-chan ifaces.ChatChunk, error) {
	ch := make(chan ifaces.ChatChunk, 1)
	ch <- ifaces.ChatChunk{Delta: "canned", Done: true}
	close(ch)
	return ch, nil
}

func (f *fakeEngine) Embedding(ctx context.Context, req *ifaces.EmbeddingRequest) (*ifaces.EmbeddingResponse, error) {
	return &ifaces.EmbeddingResponse{
		Provider:   req.Provider,
		Model:      req.ProviderModel,
		Embeddings: [][]float64{{0.1, 0.2, 0.3}},
		Usage:      ifaces.Usage{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 5},
	}, nil
}

func (f *fakeEngine) ProvidersSupported() []string {
	return []string{"fake-provider"}
}

func (f *fakeEngine) Health(ctx context.Context) ([]ifaces.ProviderHealth, error) {
	return []ifaces.ProviderHealth{
		{Provider: "fake-provider", Driver: f.name, Healthy: true, Detail: "ok"},
	}, nil
}

type fakeDriver struct {
	name string
}

func (f *fakeDriver) Name() string {
	return f.name
}

func (f *fakeDriver) New(cfg map[string]any, deps ifaces.Deps) (ifaces.Engine, error) {
	return &fakeEngine{name: f.name}, nil
}

func TestEngine_RegisterAndOpen(t *testing.T) {
	driverName := "test-fake-driver-" + t.Name()
	fd := &fakeDriver{name: driverName}

	Register(fd)

	eng, err := Open(driverName, nil, ifaces.Deps{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("Open(%q) returned error: %v", driverName, err)
	}
	if eng == nil {
		t.Fatalf("Open(%q) returned nil engine", driverName)
	}
	if eng.Name() != driverName {
		t.Fatalf("engine name mismatch: got %q, want %q", eng.Name(), driverName)
	}

	// Exercise the engine end-to-end
	resp, err := eng.ChatCompletion(context.Background(), &ifaces.ChatRequest{
		TenantID:      "tenant-1",
		Provider:      "fake-provider",
		ProviderModel: "fake-model",
		Messages:      []ifaces.ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned error: %v", err)
	}
	if resp == nil {
		t.Fatalf("ChatCompletion returned nil response")
	}
	if resp.Message.Content != "canned response" {
		t.Fatalf("unexpected response content: %q", resp.Message.Content)
	}

	// Drivers() includes the registered driver
	drivers := Drivers()
	found := false
	for _, d := range drivers {
		if d == driverName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Drivers() does not contain %q; got %v", driverName, drivers)
	}
}

func TestEngine_OpenUnknown(t *testing.T) {
	_, err := Open("nope", nil, ifaces.Deps{Logger: slog.Default()})
	if err == nil {
		t.Fatal("Open(\"nope\") expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, `unknown driver "nope"`) {
		t.Fatalf(`error message should contain 'unknown driver "nope"': %s`, errMsg)
	}
	if !strings.Contains(errMsg, "registered:") {
		t.Fatalf(`error message should list registered drivers: %s`, errMsg)
	}
}
