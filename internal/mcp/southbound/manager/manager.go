// Package manager owns Client lifecycles for downstream MCP servers. Lives
// in its own sub-package to break a would-be import cycle with the per-
// transport client packages (which depend on the southbound types).
package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	httpclient "github.com/hurtener/Portico_gateway/internal/mcp/southbound/http"
	stdiocli "github.com/hurtener/Portico_gateway/internal/mcp/southbound/stdio"
)

// Manager owns Client instances keyed by server id. Phase 1 uses a single
// shared client per server (shared_global). Phase 2 extends the key set with
// tenant + user + session for per_tenant / per_user / per_session modes.
type Manager struct {
	log     *slog.Logger
	mu      sync.RWMutex
	specs   map[string]*config.ServerSpec
	clients map[string]southbound.Client
}

// NewManager builds a Manager. The supplied specs are taken by reference; the
// Manager does not mutate them.
func NewManager(specs []config.ServerSpec, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	m := &Manager{
		log:     log,
		specs:   make(map[string]*config.ServerSpec, len(specs)),
		clients: make(map[string]southbound.Client),
	}
	for i := range specs {
		s := specs[i]
		m.specs[s.ID] = &s
	}
	return m
}

// Servers returns the configured server specs in stable id-sorted order.
// Phase 1 ignores tenant scoping; Phase 2 fixes that.
func (m *Manager) Servers() []*config.ServerSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*config.ServerSpec, 0, len(m.specs))
	for _, s := range m.specs {
		out = append(out, s)
	}
	sortByID(out)
	return out
}

// Get returns the spec for an id (for diagnostics / 404 handling).
func (m *Manager) Get(id string) (*config.ServerSpec, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.specs[id]
	return s, ok
}

// AcquireClient returns a started Client for the named server, lazily
// constructing it on first call. Subsequent calls return the cached client.
func (m *Manager) AcquireClient(ctx context.Context, serverID string) (southbound.Client, error) {
	m.mu.RLock()
	c, ok := m.clients[serverID]
	m.mu.RUnlock()
	if ok && c.Initialized() {
		return c, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[serverID]; ok && c.Initialized() {
		return c, nil
	}
	spec, ok := m.specs[serverID]
	if !ok {
		return nil, fmt.Errorf("manager: unknown server %q", serverID)
	}
	c, err := m.buildClient(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Start(ctx); err != nil {
		return nil, fmt.Errorf("manager: start %q: %w", serverID, err)
	}
	m.clients[serverID] = c
	return c, nil
}

func (m *Manager) buildClient(spec *config.ServerSpec) (southbound.Client, error) {
	switch spec.Transport {
	case "stdio":
		if spec.Stdio == nil || spec.Stdio.Command == "" {
			return nil, fmt.Errorf("manager: server %q stdio.command is required", spec.ID)
		}
		return stdiocli.New(stdiocli.Config{
			ServerID:     spec.ID,
			Command:      spec.Stdio.Command,
			Args:         spec.Stdio.Args,
			Env:          spec.Stdio.Env,
			Cwd:          spec.Stdio.Cwd,
			StartTimeout: spec.StartTimeout,
			Logger:       m.log,
		}), nil
	case "http":
		if spec.HTTP == nil || spec.HTTP.URL == "" {
			return nil, fmt.Errorf("manager: server %q http.url is required", spec.ID)
		}
		return httpclient.New(httpclient.Config{
			ServerID:   spec.ID,
			URL:        spec.HTTP.URL,
			AuthHeader: spec.HTTP.AuthHeader,
			Timeout:    spec.HTTP.Timeout,
			Logger:     m.log,
		}), nil
	default:
		return nil, fmt.Errorf("manager: server %q unsupported transport %q", spec.ID, spec.Transport)
	}
}

// CloseAll terminates every started client.
func (m *Manager) CloseAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for id, c := range m.clients {
		if err := c.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("close %q: %w", id, err))
		}
	}
	m.clients = make(map[string]southbound.Client)
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func sortByID(specs []*config.ServerSpec) {
	for i := 1; i < len(specs); i++ {
		for j := i; j > 0 && specs[j-1].ID > specs[j].ID; j-- {
			specs[j-1], specs[j] = specs[j], specs[j-1]
		}
	}
}
