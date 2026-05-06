package ifaces

import (
	"context"
	"time"
)

// ApprovalRecord mirrors one row of the approvals table. Persistence
// drivers translate between this shape and their on-disk schema.
type ApprovalRecord struct {
	ID           string
	TenantID     string
	SessionID    string
	UserID       string
	Tool         string
	ArgsSummary  string
	RiskClass    string
	Status       string
	CreatedAt    time.Time
	DecidedAt    *time.Time
	ExpiresAt    time.Time
	MetadataJSON string
}

// ApprovalStore is the persistence seam for the approval flow.
type ApprovalStore interface {
	// Insert writes a new pending approval. Idempotent on (tenant_id, id).
	Insert(ctx context.Context, a *ApprovalRecord) error

	// UpdateStatus transitions an approval's status and records the
	// decided_at timestamp. Returns ErrNotFound when the row is missing.
	UpdateStatus(ctx context.Context, tenantID, id, status string, decidedAt time.Time) error

	// Get fetches one approval. Tenant-scoped.
	Get(ctx context.Context, tenantID, id string) (*ApprovalRecord, error)

	// ListPending returns every pending approval for the tenant ordered
	// by created_at DESC.
	ListPending(ctx context.Context, tenantID string) ([]*ApprovalRecord, error)

	// ExpireOlderThan flips every pending approval whose expires_at is
	// before cutoff to status=expired. Returns the number of rows
	// modified.
	ExpireOlderThan(ctx context.Context, cutoff time.Time) (int, error)
}
