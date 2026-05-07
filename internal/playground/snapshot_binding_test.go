package playground

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

type fakeSnapshotResolver struct {
	live       *snapshots.Snapshot
	historical map[string]*snapshots.Snapshot
	createErr  error
}

func (f *fakeSnapshotResolver) Get(_ context.Context, id string) (*snapshots.Snapshot, error) {
	if s, ok := f.historical[id]; ok {
		return s, nil
	}
	return nil, snapshots.ErrNotFound
}

func (f *fakeSnapshotResolver) Create(_ context.Context, _, sessionID string) (*snapshots.Snapshot, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.live != nil {
		live := *f.live
		live.SessionID = sessionID
		return &live, nil
	}
	return &snapshots.Snapshot{ID: "live-1", TenantID: "tenant-a", OverallHash: "hash-live"}, nil
}

func TestBinding_DefaultsToLive(t *testing.T) {
	r := &fakeSnapshotResolver{}
	b := NewSnapshotBinder(r)
	bind, err := b.Bind(context.Background(), "tenant-a", "sess-1", "")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if bind.SnapshotID != "live-1" {
		t.Fatalf("expected live snapshot id, got %s", bind.SnapshotID)
	}
	if bind.IsHistorical {
		t.Fatalf("expected live, not historical")
	}
}

func TestBinding_PinnedToHistorical(t *testing.T) {
	r := &fakeSnapshotResolver{
		live: &snapshots.Snapshot{ID: "live-1", TenantID: "tenant-a", OverallHash: "hash-live"},
		historical: map[string]*snapshots.Snapshot{
			"hist-1": {ID: "hist-1", TenantID: "tenant-a", OverallHash: "hash-live"},
		},
	}
	b := NewSnapshotBinder(r)
	bind, err := b.Bind(context.Background(), "tenant-a", "sess-1", "hist-1")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !bind.IsHistorical {
		t.Fatalf("expected historical")
	}
	if bind.DriftDetected {
		t.Fatalf("hashes match — drift should be false")
	}
}

func TestBinding_DriftDetected_OnReplay(t *testing.T) {
	r := &fakeSnapshotResolver{
		live: &snapshots.Snapshot{ID: "live-1", TenantID: "tenant-a", OverallHash: "hash-NEW"},
		historical: map[string]*snapshots.Snapshot{
			"hist-1": {ID: "hist-1", TenantID: "tenant-a", OverallHash: "hash-OLD"},
		},
	}
	b := NewSnapshotBinder(r)
	bind, err := b.Bind(context.Background(), "tenant-a", "sess-1", "hist-1")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !bind.DriftDetected {
		t.Fatalf("expected drift detected (hash-OLD vs hash-NEW)")
	}
	if bind.LiveHash != "hash-NEW" || bind.OverallHash != "hash-OLD" {
		t.Fatalf("hashes mismatch: %+v", bind)
	}
}

func TestBinding_RejectsCrossTenant(t *testing.T) {
	r := &fakeSnapshotResolver{
		historical: map[string]*snapshots.Snapshot{
			"hist-1": {ID: "hist-1", TenantID: "tenant-OTHER", OverallHash: "x"},
		},
	}
	b := NewSnapshotBinder(r)
	_, err := b.Bind(context.Background(), "tenant-a", "sess-1", "hist-1")
	if err == nil {
		t.Fatalf("expected cross-tenant error")
	}
}

func TestBinding_LiveCreateError_StillReturnsHistorical(t *testing.T) {
	r := &fakeSnapshotResolver{
		createErr:  errors.New("temporarily down"),
		historical: map[string]*snapshots.Snapshot{"hist-1": {ID: "hist-1", TenantID: "tenant-a"}},
	}
	b := NewSnapshotBinder(r)
	bind, err := b.Bind(context.Background(), "tenant-a", "sess-1", "hist-1")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !bind.IsHistorical {
		t.Fatalf("expected historical bind")
	}
	if bind.DriftDetected {
		t.Fatalf("with no live we cannot detect drift")
	}
}
