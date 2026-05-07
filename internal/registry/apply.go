package registry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MutOp identifies the kind of mutation in an Apply call.
type MutOp int

// MutOp values cover the editor's operations. Restart isn't strictly a
// "mutation" in the storage sense (the spec is unchanged), but the
// supervisor reacts to a ChangeUpdated event which is what we publish for
// Restart too.
const (
	MutOpCreate  MutOp = iota + 1 // Insert; fails if id exists
	MutOpUpdate                   // Replace existing spec
	MutOpDelete                   // Remove
	MutOpRestart                  // Republish change event without spec change
)

func (o MutOp) String() string {
	switch o {
	case MutOpCreate:
		return "create"
	case MutOpUpdate:
		return "update"
	case MutOpDelete:
		return "delete"
	case MutOpRestart:
		return "restart"
	default:
		return "unknown"
	}
}

// Mutation is the unit Apply consumes. ServerSpec is non-nil for Create
// and Update; for Delete and Restart only ID is read from the spec. ActorID
// flows from the JWT through the handler.
type Mutation struct {
	Op       MutOp
	Server   *ServerSpec
	ServerID string // used for Delete + Restart
	Reason   string
	ActorID  string
}

// LogLine is one line of a server's stdout/stderr ring buffer. Phase 9
// reserves the type — the supervisor's full ring-buffer ships in a
// follow-up.
type LogLine struct {
	At     time.Time `json:"at"`
	Stream string    `json:"stream"` // "stdout"|"stderr"
	Text   string    `json:"text"`
}

// Apply is the canonical Phase 9 entry point for registry mutations. It
// wraps Upsert/Delete with an op-aware contract so REST handlers can
// dispatch through one method.
func (r *Registry) Apply(ctx context.Context, tenantID string, m Mutation) (*Snapshot, error) {
	if r == nil {
		return nil, errors.New("registry: not configured")
	}
	if tenantID == "" {
		return nil, errors.New("registry: tenant id is required")
	}
	switch m.Op {
	case MutOpCreate:
		if m.Server == nil {
			return nil, errors.New("registry: create requires server spec")
		}
		// Reject if the row already exists.
		if existing, err := r.store.GetServer(ctx, tenantID, m.Server.ID); err == nil && existing != nil {
			return nil, fmt.Errorf("registry: server %q already exists for tenant %q", m.Server.ID, tenantID)
		}
		return r.Upsert(ctx, tenantID, m.Server)
	case MutOpUpdate:
		if m.Server == nil {
			return nil, errors.New("registry: update requires server spec")
		}
		// Update is idempotent: just upsert.
		return r.Upsert(ctx, tenantID, m.Server)
	case MutOpDelete:
		id := m.ServerID
		if id == "" && m.Server != nil {
			id = m.Server.ID
		}
		if id == "" {
			return nil, errors.New("registry: delete requires server id")
		}
		if err := r.Delete(ctx, tenantID, id); err != nil {
			return nil, err
		}
		return nil, nil
	case MutOpRestart:
		id := m.ServerID
		if id == "" && m.Server != nil {
			id = m.Server.ID
		}
		if id == "" {
			return nil, errors.New("registry: restart requires server id")
		}
		return r.Restart(ctx, tenantID, id, m.Reason)
	default:
		return nil, fmt.Errorf("registry: unknown mutation op %v", m.Op)
	}
}

// Restart republishes a ChangeUpdated event for the existing spec so the
// supervisor drains and respawns the affected processes. The grace
// behaviour is owned by the supervisor / reactor.
func (r *Registry) Restart(ctx context.Context, tenantID, serverID, reason string) (*Snapshot, error) {
	if r == nil {
		return nil, errors.New("registry: not configured")
	}
	rec, err := r.store.GetServer(ctx, tenantID, serverID)
	if err != nil {
		return nil, err
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := r.store.UpsertServer(ctx, rec); err != nil {
		return nil, err
	}
	snap, err := recordToSnapshot(rec)
	if err != nil {
		return nil, err
	}
	r.publish(ChangeEvent{
		Kind:     ChangeUpdated,
		TenantID: tenantID,
		ServerID: serverID,
		New:      snap,
	})
	r.log.Info("registry: restart requested",
		"tenant_id", tenantID, "server_id", serverID, "reason", reason)
	return snap, nil
}

// Logs returns a closed channel for V1 — the supervisor's per-process
// ring buffer is a follow-up. The handler renders the existing supervisor
// status fields for the live tail; this method exists so the API surface
// is stable from Phase 9 onward.
func (r *Registry) Logs(ctx context.Context, tenantID, serverID string, _ time.Time) (<-chan LogLine, error) {
	if r == nil {
		return nil, errors.New("registry: not configured")
	}
	if _, err := r.store.GetServer(ctx, tenantID, serverID); err != nil {
		return nil, err
	}
	ch := make(chan LogLine)
	close(ch)
	_ = ctx
	return ch, nil
}
