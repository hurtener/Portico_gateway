package none_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/embeddings/none"
)

func TestNoneGenerator(t *testing.T) {
	g := none.New()
	if g.Name() != "none" {
		t.Fatalf("name: got %q", g.Name())
	}
	v, err := g.Embed(context.Background(), "t", []string{"hello"})
	if v != nil {
		t.Fatalf("expected nil vectors, got %v", v)
	}
	if !errors.Is(err, none.ErrNoEmbedder) {
		t.Fatalf("expected ErrNoEmbedder, got %v", err)
	}
}
