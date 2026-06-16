// Package engine is the driver-agnostic entry point for LLM inference engines.
//
// Callers do:
//
//	eng, err := engine.Open(cfg.Engine.Driver, cfg.Engine.Options, deps)
//
// and never import a specific engine package directly. Drivers register
// themselves at init() time via Register; the Bifrost driver (a later unit) does
// this and is brought in by a blank import in cmd/portico.
//
// This is the same §4.4 seam as internal/storage: an interface in
// internal/llm/engine/ifaces, concrete impls one level down, a factory here that
// dispatches by driver name.
package engine

import (
	"fmt"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
)

var (
	driversMu sync.RWMutex
	drivers   = map[string]ifaces.Driver{}
)

// Register installs a Driver under its Name(). Drivers MUST call Register from
// their package's init(). Re-registering panics (a programmer error).
func Register(d ifaces.Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	name := d.Name()
	if _, exists := drivers[name]; exists {
		panic(fmt.Sprintf("llm engine: driver %q already registered", name))
	}
	drivers[name] = d
}

// Drivers returns the names of registered drivers. Order is unspecified.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()
	out := make([]string, 0, len(drivers))
	for k := range drivers {
		out = append(out, k)
	}
	return out
}

// Open builds an Engine from the named driver. The driver must have been
// registered (typically by a blank import in cmd/portico).
func Open(name string, cfg map[string]any, deps ifaces.Deps) (ifaces.Engine, error) {
	driversMu.RLock()
	d, ok := drivers[name]
	driversMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("llm engine: unknown driver %q (registered: %v)", name, Drivers())
	}
	return d.New(cfg, deps)
}
