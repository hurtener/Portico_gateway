// Package apps maintains an index of every MCP App resource (`ui://`) the
// gateway has observed across downstream servers, plus the Content
// Security Policy that is composed onto served HTML payloads.
//
// The registry is in-memory and reset on restart. Phase 5 will persist
// it via a snapshot store; Phase 3 only needs live state.
package apps

import (
	"encoding/json"
	"sync"
	"time"
)

// App is the canonical record for a discovered MCP App resource.
type App struct {
	URI          string          `json:"uri"`         // canonical ui://server/path
	UpstreamURI  string          `json:"upstreamUri"` // original ui:// from the downstream
	ServerID     string          `json:"serverId"`
	Name         string          `json:"name,omitempty"`
	Description  string          `json:"description,omitempty"`
	MimeType     string          `json:"mimeType,omitempty"`
	Annotations  json.RawMessage `json:"annotations,omitempty"`
	DiscoveredAt time.Time       `json:"discoveredAt"`
}

// Registry is the live MCP App index. Safe for concurrent use.
type Registry struct {
	mu     sync.RWMutex
	items  map[string]*App
	cspCfg CSPConfig
}

// New constructs an empty Registry with the supplied CSP defaults.
func New(cfg CSPConfig) *Registry {
	return &Registry{
		items:  make(map[string]*App),
		cspCfg: cfg.WithDefaults(),
	}
}

// CSP returns the registry's default CSP config. Per-server overrides
// arrive in Phase 5 (server policy block).
func (r *Registry) CSP() CSPConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cspCfg
}

// Register upserts an app entry. Idempotent: re-registering the same
// canonical URI updates the existing record's metadata + DiscoveredAt.
func (r *Registry) Register(a *App) {
	if a == nil || a.URI == "" {
		return
	}
	if a.DiscoveredAt.IsZero() {
		a.DiscoveredAt = time.Now().UTC()
	}
	r.mu.Lock()
	r.items[a.URI] = a
	r.mu.Unlock()
}

// Lookup returns the registered app for a canonical URI, if any.
func (r *Registry) Lookup(uri string) (*App, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.items[uri]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

// List returns every registered app. The result is a copy; mutations
// after the call do not affect the registry.
func (r *Registry) List() []*App {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*App, 0, len(r.items))
	for _, a := range r.items {
		cp := *a
		out = append(out, &cp)
	}
	return out
}

// ListByServer filters the registry to apps from a specific server.
// Phase 5 will add a tenant-scoped variant that intersects with policy.
func (r *Registry) ListByServer(serverID string) []*App {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*App, 0)
	for _, a := range r.items {
		if a.ServerID == serverID {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out
}

// Forget removes every entry for a server (called when the server is
// deleted from the registry — see registry.Reactor).
func (r *Registry) Forget(serverID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for k, a := range r.items {
		if a.ServerID == serverID {
			delete(r.items, k)
			removed++
		}
	}
	return removed
}
