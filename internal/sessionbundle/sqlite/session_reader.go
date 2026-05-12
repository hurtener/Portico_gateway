// session_reader.go reads single rows from the `sessions` table for
// the bundle Loader. The existing snapshot store doesn't expose a
// "give me one row" path, so the loader gets its own thin reader.
//
// Phase 11.

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/sessionbundle"
)

// SessionReader implements sessionbundle.SessionReader against the
// SQLite-backed sessions table. Tenant scoping is enforced in the
// WHERE clause so cross-tenant lookups never match a row.
type SessionReader struct {
	db *sql.DB
}

// NewSessionReader builds the reader. The DB must already have the
// 0001_init migration applied.
func NewSessionReader(db *sql.DB) *SessionReader { return &SessionReader{db: db} }

// GetSession returns the (tenant, session) row or (nil, nil) when
// missing. The bundle Loader treats nil as ErrSessionNotFound — this
// reader stays out of the policy of "what to do with missing rows".
func (r *SessionReader) GetSession(ctx context.Context, tenantID, sessionID string) (*sessionbundle.SessionRow, error) {
	if tenantID == "" || sessionID == "" {
		return nil, errors.New("sessionbundle/sqlite: tenant + session required")
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id,
		       COALESCE(user_id, ''),
		       COALESCE(snapshot_id, ''),
		       started_at,
		       COALESCE(ended_at, ''),
		       COALESCE(metadata_json, '')
		  FROM sessions
		 WHERE tenant_id = ? AND id = ?
	`, tenantID, sessionID)

	var (
		id, tid, uid, sid, started, ended, meta string
	)
	if err := row.Scan(&id, &tid, &uid, &sid, &started, &ended, &meta); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("session read: %w", err)
	}

	out := &sessionbundle.SessionRow{
		ID:         id,
		TenantID:   tid,
		UserID:     uid,
		SnapshotID: sid,
	}
	if t, err := parseTime(started); err == nil {
		out.StartedAt = t
	}
	if ended != "" {
		if t, err := parseTime(ended); err == nil {
			out.EndedAt = t
		}
	}
	if meta != "" && json.Valid([]byte(meta)) {
		out.Metadata = json.RawMessage(meta)
	}
	return out, nil
}

// parseTime accepts either RFC3339Nano or the SQLite literal format
// the snapshot store writes.
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05.000Z", s)
}
