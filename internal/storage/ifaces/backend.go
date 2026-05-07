package ifaces

import (
	"context"
	"errors"
)

// ErrNotFound is the canonical "row not present" error returned by stores.
// All concrete drivers MUST return this exact error (or wrap it with %w) so
// handlers can branch on it without importing the driver package.
var ErrNotFound = errors.New("storage: not found")

// Backend is the storage protocol. Concrete drivers (sqlite, postgres,
// future external proxies) implement it. Code outside internal/storage/<driver>/
// must depend on this interface, not on a concrete driver.
//
// New drivers register a factory via internal/storage.Register; callers obtain
// a Backend through internal/storage.Open which dispatches by driver name.
type Backend interface {
	// Tenants returns the per-tenant CRUD store.
	Tenants() TenantStore

	// Audit returns the audit event store.
	Audit() AuditStore

	// Registry returns the server / instance registry store (Phase 2+).
	Registry() RegistryStore

	// Skills returns the per-skill enablement store (Phase 4+).
	Skills() SkillEnablementStore

	// Approvals returns the per-tenant approval store.
	Approvals() ApprovalStore

	// Snapshots returns the catalog-snapshot + fingerprint store.
	Snapshots() SnapshotStore

	// SkillSources returns the tenant-scoped skill-source registry
	// store (Phase 8).
	SkillSources() SkillSourceStore

	// AuthoredSkills returns the tenant-scoped authored skill store
	// (Phase 8).
	AuthoredSkills() AuthoredSkillStore

	// Health pings the underlying connection. Returns error if the backend
	// is not reachable. Used by /readyz and tests.
	Health(ctx context.Context) error

	// Driver names the backend ("sqlite", "postgres", ...). Useful for logs.
	Driver() string

	// Close releases connections and any background goroutines.
	Close() error
}
