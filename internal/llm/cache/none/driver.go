// Package none is the default no-op semantic-cache driver: every lookup misses
// and every store is discarded. Selecting it (or leaving the cache
// unconfigured) makes the LLM gateway behave exactly as if there were no cache.
package none

import (
	"context"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

func init() {
	cache.Register("none", func(_ map[string]any, _ ifaces.Deps) (ifaces.Cache, error) {
		return noopCache{}, nil
	})
}

type noopCache struct{}

func (noopCache) Name() string { return "none" }

func (noopCache) Lookup(_ context.Context, _ ifaces.Key, _ ifaces.LookupOpts) (*ifaces.Entry, bool, error) {
	return nil, false, nil
}

func (noopCache) Store(_ context.Context, _ ifaces.Key, _ ifaces.Entry) error { return nil }

func (noopCache) Invalidate(_ context.Context, _ ifaces.Prefix) (int, error) { return 0, nil }

func (noopCache) Stats(_ context.Context, _ string) (ifaces.Stats, error) {
	return ifaces.Stats{Driver: "none"}, nil
}

func (noopCache) Close(_ context.Context) error { return nil }
