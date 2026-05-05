// Package ifaces declares the storage interfaces consumed across the Portico
// codebase. Concrete implementations live in internal/storage/sqlite (and
// post-V1, internal/storage/postgres).
package ifaces

import (
	"context"
	"time"
)

// Tenant is the domain object for a tenant. JSON tags use snake_case so the
// REST API surface matches the rest of the project.
type Tenant struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	Plan        string    `json:"plan"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TenantStore persists tenant records.
type TenantStore interface {
	Get(ctx context.Context, id string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Upsert(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
}
