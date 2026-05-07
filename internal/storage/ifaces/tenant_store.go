// Package ifaces declares the storage interfaces consumed across the Portico
// codebase. Concrete implementations live in internal/storage/sqlite (and
// post-V1, internal/storage/postgres).
package ifaces

import (
	"context"
	"time"
)

// Tenant is the domain object for a tenant. JSON tags use snake_case so the
// REST API surface matches the rest of the project. Phase 9 adds the runtime
// configuration fields managed from the Console; existing fields are
// preserved for back-compat.
type Tenant struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Plan        string `json:"plan"`

	// Phase 9: runtime configuration. New fields default to safe values when
	// rows pre-date the migration.
	RuntimeMode           string `json:"runtime_mode"`
	MaxConcurrentSessions int    `json:"max_concurrent_sessions"`
	MaxRequestsPerMinute  int    `json:"max_requests_per_minute"`
	AuditRetentionDays    int    `json:"audit_retention_days"`
	JWTIssuer             string `json:"jwt_issuer"`
	JWTJWKSURL            string `json:"jwt_jwks_url"`
	Status                string `json:"status"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TenantStore persists tenant records.
type TenantStore interface {
	Get(ctx context.Context, id string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Upsert(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
}
