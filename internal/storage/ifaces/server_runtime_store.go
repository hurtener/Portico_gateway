package ifaces

import (
	"context"
	"time"
)

// ServerRuntimeRecord captures the runtime overrides Phase 9 introduces:
// per-tenant + per-server env overrides, an enabled flag (mirrors the
// servers row but lives separate so toggles are cheap), and bookkeeping for
// the most recent restart.
type ServerRuntimeRecord struct {
	TenantID          string    `json:"tenant_id"`
	ServerID          string    `json:"server_id"`
	EnvOverrides      []byte    `json:"-"`
	Enabled           bool      `json:"enabled"`
	LastRestartAt     time.Time `json:"last_restart_at,omitempty"`
	LastRestartReason string    `json:"last_restart_reason,omitempty"`
}

// ServerRuntimeStore persists ServerRuntimeRecord rows.
type ServerRuntimeStore interface {
	Get(ctx context.Context, tenantID, serverID string) (*ServerRuntimeRecord, error)
	Upsert(ctx context.Context, r *ServerRuntimeRecord) error
	Delete(ctx context.Context, tenantID, serverID string) error
	List(ctx context.Context, tenantID string) ([]*ServerRuntimeRecord, error)
	// RecordRestart updates the last_restart_at + reason in-place. Used by
	// the registry's restart path so the Console can render a "last
	// restarted at" badge.
	RecordRestart(ctx context.Context, tenantID, serverID, reason string, at time.Time) error
}
