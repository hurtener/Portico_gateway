// Package storage is the driver-agnostic entry point for persistence.
//
// Callers do:
//
//	backend, err := storage.Open(ctx, cfg.Storage, log)
//
// and never import a specific driver package directly. New drivers register
// themselves at init() time via Register; the standard SQLite driver in
// internal/storage/sqlite/ does this and is brought in by a blank import in
// cmd/portico (or any test that needs storage).
//
// This is the canonical "easy seam" pattern for Portico: an interface in
// internal/.../ifaces/, concrete impls one level down, a factory at the
// package root that dispatches by driver/strategy name. The same shape is
// used (or will be used) by the credential vault, skill sources, MCP
// transports, etc.
package storage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Factory builds a Backend from the storage config.
type Factory func(ctx context.Context, cfg config.StorageConfig, log *slog.Logger) (ifaces.Backend, error)

var (
	factoriesMu sync.RWMutex
	factories   = map[string]Factory{}
)

// Register installs a Factory under the given driver name. Drivers MUST call
// Register from their package's init() function. Re-registering panics
// (factory conflicts are programmer errors).
func Register(name string, f Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("storage: driver %q already registered", name))
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

// Open returns a Backend for the given storage config. The driver must have
// been registered via Register (typically by a blank import in cmd/portico).
func Open(ctx context.Context, cfg config.StorageConfig, log *slog.Logger) (ifaces.Backend, error) {
	factoriesMu.RLock()
	f, ok := factories[cfg.Driver]
	factoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("storage: unknown driver %q (registered: %v)", cfg.Driver, Drivers())
	}
	return f(ctx, cfg, log)
}
