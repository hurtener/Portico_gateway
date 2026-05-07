package ifaces

import (
	"context"
	"time"
)

// EntityActivityRecord is one row in the entity_activity projection. The
// audit_events table remains canonical; this projection is what the Console
// "Activity" tab renders.
type EntityActivityRecord struct {
	TenantID    string    `json:"tenant_id"`
	EntityKind  string    `json:"entity_kind"`
	EntityID    string    `json:"entity_id"`
	EventID     string    `json:"event_id"`
	OccurredAt  time.Time `json:"occurred_at"`
	ActorUserID string    `json:"actor_user_id,omitempty"`
	Summary     string    `json:"summary"`
	DiffJSON    []byte    `json:"-"`
}

// EntityActivityStore writes + reads activity projection rows.
type EntityActivityStore interface {
	Append(ctx context.Context, r *EntityActivityRecord) error
	List(ctx context.Context, tenantID, kind, id string, limit int) ([]*EntityActivityRecord, error)
}
