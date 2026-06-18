// Package ifaces defines the embedding-generator seam used by semantic cache
// drivers. The default production generator routes through the LLM gateway so
// Portico-managed credentials apply (wired in a later unit); this package and
// the none generator are dependency-free.
package ifaces

import "context"

// EmbeddingGenerator turns prompt text into vectors for similarity lookup.
// Implementations must be safe for concurrent use.
type EmbeddingGenerator interface {
	// Name identifies the generator (for logs / stats).
	Name() string
	// Embed returns one vector per input string, in input order.
	Embed(ctx context.Context, tenantID string, input []string) ([][]float32, error)
}
