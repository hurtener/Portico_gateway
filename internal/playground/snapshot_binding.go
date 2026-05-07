package playground

import (
	"context"
	"errors"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

// SnapshotResolver is the slim seam StoreLookup the binder uses. The
// real implementation is *snapshots.Service.
type SnapshotResolver interface {
	Get(ctx context.Context, id string) (*snapshots.Snapshot, error)
	Create(ctx context.Context, tenantID, sessionID string) (*snapshots.Snapshot, error)
}

// Binding is the per-session pin to a catalog snapshot.
type Binding struct {
	SnapshotID    string
	TenantID      string
	SessionID     string
	BoundAt       time.Time
	OverallHash   string
	IsHistorical  bool
	DriftDetected bool
	LiveHash      string
}

// SnapshotBinder is the playground's view of Phase 6 snapshot binding.
// It defaults to live (Create a fresh snapshot) and supports operator-
// pinned historical snapshots.
type SnapshotBinder struct {
	resolver SnapshotResolver
}

// NewSnapshotBinder constructs a binder over the given resolver.
func NewSnapshotBinder(r SnapshotResolver) *SnapshotBinder {
	return &SnapshotBinder{resolver: r}
}

// Bind resolves the requested snapshot for the session. When pinnedID is
// non-empty the binder loads that historical snapshot AND a fresh live
// one to detect drift. When empty the binder simply creates a live
// snapshot for the session.
func (b *SnapshotBinder) Bind(ctx context.Context, tenantID, sessionID, pinnedID string) (*Binding, error) {
	if b == nil || b.resolver == nil {
		return nil, errors.New("playground: snapshot binder not configured")
	}
	if tenantID == "" {
		return nil, errors.New("playground: tenant_id required")
	}
	if pinnedID == "" {
		live, err := b.resolver.Create(ctx, tenantID, sessionID)
		if err != nil {
			return nil, err
		}
		return &Binding{
			SnapshotID:  live.ID,
			TenantID:    tenantID,
			SessionID:   sessionID,
			BoundAt:     time.Now().UTC(),
			OverallHash: live.OverallHash,
			LiveHash:    live.OverallHash,
		}, nil
	}
	// Historical pin: fetch the saved snapshot and a live one for drift.
	pinned, err := b.resolver.Get(ctx, pinnedID)
	if err != nil {
		return nil, err
	}
	if pinned.TenantID != tenantID {
		return nil, errors.New("playground: snapshot belongs to a different tenant")
	}
	live, err := b.resolver.Create(ctx, tenantID, sessionID)
	if err != nil {
		// Failure to recompute live shouldn't block the binding —
		// historical replay still works, just no drift detection.
		return &Binding{
			SnapshotID:   pinned.ID,
			TenantID:     tenantID,
			SessionID:    sessionID,
			BoundAt:      time.Now().UTC(),
			OverallHash:  pinned.OverallHash,
			IsHistorical: true,
		}, nil
	}
	bind := &Binding{
		SnapshotID:   pinned.ID,
		TenantID:     tenantID,
		SessionID:    sessionID,
		BoundAt:      time.Now().UTC(),
		OverallHash:  pinned.OverallHash,
		LiveHash:     live.OverallHash,
		IsHistorical: true,
	}
	if pinned.OverallHash != "" && live.OverallHash != "" && pinned.OverallHash != live.OverallHash {
		bind.DriftDetected = true
	}
	return bind, nil
}
