// Package secrets owns Portico's credential vault. Phase 2 ships a file-
// backed stub keyed by an AES-256-GCM master key from PORTICO_VAULT_KEY.
// Phase 5 will plug in real OAuth flows (RFC 8693 token exchange) and
// per-tenant alternate backends behind the same Vault interface.
package secrets

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Vault.Get when a (tenant, name) pair is
// absent. Callers wrap it (errors.Is) rather than string-comparing.
var ErrNotFound = errors.New("secrets: not found")

// Vault is the protocol every credential backend implements. Tenant scope
// is enforced by every call: a tenant cannot read another tenant's secrets.
type Vault interface {
	Get(ctx context.Context, tenantID, name string) (string, error)
	Put(ctx context.Context, tenantID, name, value string) error
	Delete(ctx context.Context, tenantID, name string) error
	List(ctx context.Context, tenantID string) ([]string, error)
	Close() error
}
