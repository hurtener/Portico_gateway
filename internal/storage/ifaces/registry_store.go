package ifaces

import (
	"context"
	"encoding/json"
	"time"
)

// ServerRecord is the storage representation of a registered MCP server.
// Mirror of the Phase 2 plan's ServerRecord; lives here because the registry
// package's RegistryStore depends on it.
type ServerRecord struct {
	TenantID     string          `json:"tenant_id"`
	ID           string          `json:"id"`
	DisplayName  string          `json:"display_name"`
	Transport    string          `json:"transport"`
	RuntimeMode  string          `json:"runtime_mode"`
	Spec         json.RawMessage `json:"spec"`
	Enabled      bool            `json:"enabled"`
	Status       string          `json:"status"`
	StatusDetail string          `json:"status_detail,omitempty"`
	SchemaHash   string          `json:"schema_hash,omitempty"`
	LastError    string          `json:"last_error,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// InstanceRecord is the supervisor's per-instance bookkeeping row.
type InstanceRecord struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	ServerID     string    `json:"server_id"`
	UserID       string    `json:"user_id,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	PID          int       `json:"pid"`
	StartedAt    time.Time `json:"started_at"`
	LastCallAt   time.Time `json:"last_call_at,omitempty"`
	State        string    `json:"state"`
	RestartCount int       `json:"restart_count"`
	LastError    string    `json:"last_error,omitempty"`
	SchemaHash   string    `json:"schema_hash,omitempty"`
}

// RegistryStore persists ServerRecord + InstanceRecord rows. Tenant-scoped
// for every read/write — cross-tenant access happens only via admin scope
// at the API layer, which calls List/Get with the target tenant id.
type RegistryStore interface {
	// Server CRUD
	GetServer(ctx context.Context, tenantID, id string) (*ServerRecord, error)
	ListServers(ctx context.Context, tenantID string) ([]*ServerRecord, error)
	UpsertServer(ctx context.Context, r *ServerRecord) error
	DeleteServer(ctx context.Context, tenantID, id string) error
	UpdateServerStatus(ctx context.Context, tenantID, id, status, detail string) error

	// Instance CRUD. DeleteInstance takes tenantID per CLAUDE.md §6 —
	// instance ids alone are random but the storage layer must enforce
	// the tenant invariant.
	UpsertInstance(ctx context.Context, i *InstanceRecord) error
	DeleteInstance(ctx context.Context, tenantID, id string) error
	ListInstances(ctx context.Context, tenantID, serverID string) ([]*InstanceRecord, error)
}
