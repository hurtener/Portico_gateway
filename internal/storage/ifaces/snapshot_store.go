package ifaces

import (
	"context"
	"time"
)

// SnapshotRecord mirrors one row of the catalog_snapshots table.
type SnapshotRecord struct {
	ID          string
	TenantID    string
	SessionID   string
	OverallHash string
	PayloadJSON string
	CreatedAt   time.Time
}

// FingerprintRecord mirrors one row of schema_fingerprints. Used by the
// drift detector to bookkeep recent server hashes outside the snapshot
// row so a bursty downstream doesn't bloat the snapshot table.
type FingerprintRecord struct {
	TenantID   string
	ServerID   string
	Hash       string
	ToolsCount int
	SeenAt     time.Time
}

// ActiveSessionRecord is the (sessionID, tenantID, snapshotID) tuple the
// drift detector reads. Filtered to sessions with no ended_at.
type ActiveSessionRecord struct {
	SessionID  string
	TenantID   string
	SnapshotID string
	StartedAt  time.Time
}

// SnapshotListQuery filters snapshot lookups.
type SnapshotListQuery struct {
	Since  time.Time
	Until  time.Time
	Limit  int
	Cursor string
}

// SnapshotStore is the persistence seam for catalog snapshots and the
// per-server schema fingerprint cache.
type SnapshotStore interface {
	Insert(ctx context.Context, r *SnapshotRecord) error
	Get(ctx context.Context, id string) (*SnapshotRecord, error)
	List(ctx context.Context, tenantID string, q SnapshotListQuery) ([]*SnapshotRecord, string, error)
	StampSession(ctx context.Context, sessionID, snapshotID string) error
	UpsertFingerprint(ctx context.Context, r *FingerprintRecord) error
	LatestFingerprint(ctx context.Context, tenantID, serverID string) (*FingerprintRecord, error)
	ActiveSessions(ctx context.Context, since time.Time) ([]ActiveSessionRecord, error)
	// CloseSession marks a session ended so the drift detector skips it.
	CloseSession(ctx context.Context, sessionID string) error
}
