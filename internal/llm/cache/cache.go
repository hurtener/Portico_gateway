// Package cache is the driver-agnostic entry point for the LLM semantic cache.
//
// Callers do:
//
//	c, err := cache.Open(cfg.Driver, cfg.Options, cache.ifaces.Deps{Embedder: gen})
//
// and never import a specific driver package directly. New drivers register
// themselves at init() time via Register; the standard drivers (none, inmem,
// redis, weaviate, qdrant) do this and are brought in by blank imports in
// cmd/portico.
//
// This is the canonical §4.4 seam: an interface in internal/llm/cache/ifaces/,
// concrete impls one level down, a factory at the package root that dispatches
// by driver name. It mirrors internal/storage/storage.go exactly.
package cache

import (
	"fmt"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// Factory builds a Cache from an opaque per-driver config block + deps.
type Factory func(cfg map[string]any, deps ifaces.Deps) (ifaces.Cache, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]Factory{}
)

// Register installs a Factory under the given driver name. Drivers MUST call
// Register from their package's init() function. Re-registering panics (factory
// conflicts are programmer errors).
func Register(name string, f Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("cache: driver %q already registered", name))
	}
	factories[name] = f
}

// Drivers returns the names of registered drivers. Order is unspecified.
func Drivers() []string {
	factoriesMu.RLock()
	defer factoriesMu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	return out
}

// Open returns a Cache for the given driver name. The driver must have been
// registered via Register (typically by a blank import in cmd/portico). An empty
// name selects the "none" driver so an unconfigured cache is a safe no-op.
func Open(name string, cfg map[string]any, deps ifaces.Deps) (ifaces.Cache, error) {
	if name == "" {
		name = "none"
	}
	factoriesMu.RLock()
	f, ok := factories[name]
	factoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cache: unknown driver %q (registered: %v)", name, Drivers())
	}
	return f(cfg, deps)
}
