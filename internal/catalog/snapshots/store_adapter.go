package snapshots

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// NewStorageAdapter wraps an ifaces.SnapshotStore so it satisfies the
// snapshot package's narrower Store interface. Lives here so the
// package stays free of the storage import surface beyond this file.
func NewStorageAdapter(s ifaces.SnapshotStore) Store {
	return &storageAdapter{s: s}
}

type storageAdapter struct {
	s ifaces.SnapshotStore
}

func (a *storageAdapter) Insert(ctx context.Context, snap *Snapshot) error {
	if a == nil || a.s == nil {
		return errors.New("snapshots: adapter not configured")
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return a.s.Insert(ctx, &ifaces.SnapshotRecord{
		ID:          snap.ID,
		TenantID:    snap.TenantID,
		SessionID:   snap.SessionID,
		OverallHash: snap.OverallHash,
		PayloadJSON: string(body),
		CreatedAt:   snap.CreatedAt,
	})
}

func (a *storageAdapter) Get(ctx context.Context, id string) (*Snapshot, error) {
	rec, err := a.s.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ifaces.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return decodeSnapshot(rec)
}

func (a *storageAdapter) List(ctx context.Context, tenantID string, q ListQuery) ([]*Snapshot, string, error) {
	recs, next, err := a.s.List(ctx, tenantID, ifaces.SnapshotListQuery{
		Since:  q.Since,
		Until:  q.Until,
		Limit:  q.Limit,
		Cursor: q.Cursor,
	})
	if err != nil {
		return nil, "", err
	}
	out := make([]*Snapshot, 0, len(recs))
	for _, r := range recs {
		s, err := decodeSnapshot(r)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, next, nil
}

func (a *storageAdapter) StampSession(ctx context.Context, sessionID, snapshotID string) error {
	return a.s.StampSession(ctx, sessionID, snapshotID)
}

func (a *storageAdapter) UpsertFingerprint(ctx context.Context, tenantID, serverID, hash string, toolsCount int) error {
	return a.s.UpsertFingerprint(ctx, &ifaces.FingerprintRecord{
		TenantID:   tenantID,
		ServerID:   serverID,
		Hash:       hash,
		ToolsCount: toolsCount,
		SeenAt:     time.Now().UTC(),
	})
}

func (a *storageAdapter) LatestFingerprint(ctx context.Context, tenantID, serverID string) (string, error) {
	rec, err := a.s.LatestFingerprint(ctx, tenantID, serverID)
	if err != nil {
		if errors.Is(err, ifaces.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return rec.Hash, nil
}

func (a *storageAdapter) ActiveSessions(ctx context.Context, since time.Time) ([]ActiveSession, error) {
	recs, err := a.s.ActiveSessions(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]ActiveSession, 0, len(recs))
	for _, r := range recs {
		out = append(out, ActiveSession{
			SessionID:  r.SessionID,
			TenantID:   r.TenantID,
			SnapshotID: r.SnapshotID,
			StartedAt:  r.StartedAt,
		})
	}
	return out, nil
}

func decodeSnapshot(r *ifaces.SnapshotRecord) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal([]byte(r.PayloadJSON), &s); err != nil {
		return nil, err
	}
	if s.ID == "" {
		s.ID = r.ID
	}
	if s.TenantID == "" {
		s.TenantID = r.TenantID
	}
	if s.SessionID == "" {
		s.SessionID = r.SessionID
	}
	if s.OverallHash == "" {
		s.OverallHash = r.OverallHash
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = r.CreatedAt
	}
	return &s, nil
}
