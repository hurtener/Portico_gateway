// Package manager owns Client lifecycles for downstream MCP servers. In
// Phase 2 it became a thin coordinator: the Registry holds per-tenant
// specs, the Supervisor owns the live process state, and the Manager
// translates a session-scoped Acquire request into a (key, client) pair
// for the dispatcher.
//
// Lives in its own sub-package to break a would-be import cycle with the
// per-transport client packages (which depend on the southbound types).
package manager

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// AcquireRequest carries the per-call identity needed to compute the
// supervisor's InstanceKey. Sourced from the MCP session.
type AcquireRequest struct {
	TenantID  string
	UserID    string
	SessionID string
	ServerID  string
}

// Manager fronts the supervisor and registry for the dispatcher.
type Manager struct {
	log *slog.Logger
	reg *registry.Registry
	sup *process.Supervisor
}

// NewManager wires the supervisor + registry. Either may be nil only in
// tests that don't exercise tool routing — production callers must
// supply both.
func NewManager(reg *registry.Registry, sup *process.Supervisor, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{log: log, reg: reg, sup: sup}
}

// Servers returns the snapshot list for the given tenant.
func (m *Manager) Servers(ctx context.Context, tenantID string) ([]*registry.Snapshot, error) {
	if m.reg == nil {
		return nil, errors.New("manager: registry not configured")
	}
	if tenantID == "" {
		// Defensive: shouldn't happen in production paths but keeps tests
		// that use empty-tenant scaffolding from panicking.
		return []*registry.Snapshot{}, nil
	}
	return m.reg.List(ctx, tenantID)
}

// Get returns the snapshot for (tenant, server). ifaces.ErrNotFound when
// the server is not registered for the tenant.
func (m *Manager) Get(ctx context.Context, tenantID, serverID string) (*registry.Snapshot, error) {
	if m.reg == nil {
		return nil, errors.New("manager: registry not configured")
	}
	return m.reg.Get(ctx, tenantID, serverID)
}

// Acquire returns a started Client for (tenant, server, identity).
// Lazily spawns via the supervisor on first use.
func (m *Manager) Acquire(ctx context.Context, req AcquireRequest) (southbound.Client, error) {
	if m.sup == nil {
		return nil, errors.New("manager: supervisor not configured")
	}
	snap, err := m.Get(ctx, req.TenantID, req.ServerID)
	if err != nil {
		return nil, err
	}
	if !snap.Record.Enabled {
		return nil, errors.New("manager: server is disabled")
	}
	return m.sup.Acquire(ctx, &snap.Spec, process.AcquireOpts{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		SessionID: req.SessionID,
	})
}

// Tick informs the supervisor that the caller successfully made a tool
// call against the given server, so idle/last-call bookkeeping updates.
func (m *Manager) Tick(ctx context.Context, req AcquireRequest) {
	if m.sup == nil {
		return
	}
	snap, err := m.Get(ctx, req.TenantID, req.ServerID)
	if err != nil {
		return
	}
	key, err := process.KeyForMode(snap.Spec.RuntimeMode, snap.Spec.ID, process.AcquireOpts{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		SessionID: req.SessionID,
	})
	if err != nil {
		return
	}
	m.sup.Tick(ctx, key)
}

// CloseAll terminates every supervised instance.
func (m *Manager) CloseAll(ctx context.Context) error {
	if m.sup == nil {
		return nil
	}
	return m.sup.StopAll(ctx)
}

// SnapshotByTenant is a convenience for callers that only need the spec
// list (no client acquisition).
func (m *Manager) SnapshotByTenant(ctx context.Context, tenantID string) ([]*registry.Snapshot, error) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}
	return m.Servers(ctx, tenantID)
}

// IfacesErrNotFound is re-exported for callers that branch on it without
// importing storage/ifaces directly.
var IfacesErrNotFound = ifaces.ErrNotFound
