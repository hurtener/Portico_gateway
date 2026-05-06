package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type snapshotStore struct {
	db *sql.DB
}

const sqliteTimeFormat = "2006-01-02T15:04:05.000Z"

func (s *snapshotStore) Insert(ctx context.Context, r *ifaces.SnapshotRecord) error {
	if r == nil {
		return errors.New("sqlite: nil snapshot")
	}
	if r.ID == "" || r.TenantID == "" {
		return errors.New("sqlite: snapshot requires id + tenant_id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO catalog_snapshots(id, tenant_id, session_id, payload_json, created_at, overall_hash)
		VALUES (?, ?, NULLIF(?, ''), ?, ?, ?)
	`,
		r.ID, r.TenantID, r.SessionID, r.PayloadJSON,
		r.CreatedAt.UTC().Format(sqliteTimeFormat),
		r.OverallHash,
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert snapshot: %w", err)
	}
	return nil
}

func (s *snapshotStore) Get(ctx context.Context, id string) (*ifaces.SnapshotRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, session_id, payload_json, created_at, COALESCE(overall_hash, '')
		FROM catalog_snapshots WHERE id = ?
	`, id)
	return scanSnapshot(row)
}

func (s *snapshotStore) List(ctx context.Context, tenantID string, q ifaces.SnapshotListQuery) ([]*ifaces.SnapshotRecord, string, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	args := []any{tenantID}
	where := "WHERE tenant_id = ?"
	if !q.Since.IsZero() {
		where += " AND created_at >= ?"
		args = append(args, q.Since.UTC().Format(sqliteTimeFormat))
	}
	if !q.Until.IsZero() {
		where += " AND created_at < ?"
		args = append(args, q.Until.UTC().Format(sqliteTimeFormat))
	}
	if q.Cursor != "" {
		where += " AND id < ?"
		args = append(args, q.Cursor)
	}
	args = append(args, q.Limit)
	//nolint:gosec // dynamic clause assembly with parameterised values
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, session_id, payload_json, created_at, COALESCE(overall_hash, '')
		 FROM catalog_snapshots `+where+`
		 ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]*ifaces.SnapshotRecord, 0, q.Limit)
	for rows.Next() {
		r, err := scanSnapshot(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) == q.Limit {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

func (s *snapshotStore) StampSession(ctx context.Context, sessionID, snapshotID string) error {
	if sessionID == "" || snapshotID == "" {
		return errors.New("sqlite: stamp session requires both ids")
	}
	// Insert the session row if it doesn't exist (the northbound transport
	// owns sessions in memory; persistence is best-effort for snapshot
	// linkage and drift bookkeeping). We need tenant_id, so look up the
	// snapshot to find it.
	var tenantID string
	if err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM catalog_snapshots WHERE id = ?`, snapshotID).Scan(&tenantID); err != nil {
		return fmt.Errorf("sqlite: stamp session: lookup snapshot: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions(id, tenant_id, snapshot_id, started_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET snapshot_id = excluded.snapshot_id
	`, sessionID, tenantID, snapshotID, time.Now().UTC().Format(sqliteTimeFormat))
	if err != nil {
		return fmt.Errorf("sqlite: stamp session: %w", err)
	}
	return nil
}

func (s *snapshotStore) UpsertFingerprint(ctx context.Context, r *ifaces.FingerprintRecord) error {
	if r == nil {
		return errors.New("sqlite: nil fingerprint")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO schema_fingerprints(tenant_id, server_id, hash, tools_count, seen_at)
		VALUES (?, ?, ?, ?, ?)
	`, r.TenantID, r.ServerID, r.Hash, r.ToolsCount,
		time.Now().UTC().Format(sqliteTimeFormat))
	if err != nil {
		return fmt.Errorf("sqlite: upsert fingerprint: %w", err)
	}
	return nil
}

func (s *snapshotStore) LatestFingerprint(ctx context.Context, tenantID, serverID string) (*ifaces.FingerprintRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, server_id, hash, tools_count, seen_at
		FROM schema_fingerprints
		WHERE tenant_id = ? AND server_id = ?
		ORDER BY seen_at DESC LIMIT 1
	`, tenantID, serverID)
	var (
		r       ifaces.FingerprintRecord
		seenStr string
	)
	if err := row.Scan(&r.TenantID, &r.ServerID, &r.Hash, &r.ToolsCount, &seenStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrNotFound
		}
		return nil, err
	}
	if t, err := time.Parse(sqliteTimeFormat, seenStr); err == nil {
		r.SeenAt = t
	}
	return &r, nil
}

func (s *snapshotStore) ActiveSessions(ctx context.Context, since time.Time) ([]ifaces.ActiveSessionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, COALESCE(snapshot_id, ''), started_at
		FROM sessions
		WHERE ended_at IS NULL AND started_at >= ?
	`, since.UTC().Format(sqliteTimeFormat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ifaces.ActiveSessionRecord, 0)
	for rows.Next() {
		var (
			r    ifaces.ActiveSessionRecord
			sStr string
		)
		if err := rows.Scan(&r.SessionID, &r.TenantID, &r.SnapshotID, &sStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse(sqliteTimeFormat, sStr); err == nil {
			r.StartedAt = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *snapshotStore) CloseSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET ended_at = ? WHERE id = ? AND ended_at IS NULL
	`, time.Now().UTC().Format(sqliteTimeFormat), sessionID)
	if err != nil {
		return fmt.Errorf("sqlite: close session: %w", err)
	}
	return nil
}

func scanSnapshot(s scanner) (*ifaces.SnapshotRecord, error) {
	var (
		r       ifaces.SnapshotRecord
		sess    sql.NullString
		created string
		hash    string
	)
	if err := s.Scan(&r.ID, &r.TenantID, &sess, &r.PayloadJSON, &created, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrNotFound
		}
		return nil, fmt.Errorf("sqlite: scan snapshot: %w", err)
	}
	r.SessionID = sess.String
	r.OverallHash = hash
	if t, err := time.Parse(sqliteTimeFormat, created); err == nil {
		r.CreatedAt = t
	}
	return &r, nil
}
