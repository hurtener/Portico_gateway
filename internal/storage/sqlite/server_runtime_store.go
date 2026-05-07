package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type serverRuntimeStore struct {
	db *sql.DB
}

const serverRuntimeSelect = `
	SELECT tenant_id, server_id, env_overrides, enabled,
	       COALESCE(last_restart_at, ''), COALESCE(last_restart_reason, '')
	FROM tenant_servers_runtime
`

func (s *serverRuntimeStore) Get(ctx context.Context, tenantID, serverID string) (*ifaces.ServerRuntimeRecord, error) {
	row := s.db.QueryRowContext(ctx,
		serverRuntimeSelect+`WHERE tenant_id = ? AND server_id = ?`,
		tenantID, serverID)
	r, err := scanServerRuntime(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	return r, err
}

func (s *serverRuntimeStore) Upsert(ctx context.Context, r *ifaces.ServerRuntimeRecord) error {
	if r == nil || r.TenantID == "" || r.ServerID == "" {
		return errors.New("sqlite: tenant_id and server_id required")
	}
	envText := string(r.EnvOverrides)
	if envText == "" {
		envText = "{}"
	}
	var lastRestart any
	if !r.LastRestartAt.IsZero() {
		lastRestart = r.LastRestartAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_servers_runtime
		    (tenant_id, server_id, env_overrides, enabled, last_restart_at, last_restart_reason)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, server_id) DO UPDATE SET
		    env_overrides       = excluded.env_overrides,
		    enabled             = excluded.enabled,
		    last_restart_at     = COALESCE(excluded.last_restart_at, tenant_servers_runtime.last_restart_at),
		    last_restart_reason = COALESCE(excluded.last_restart_reason, tenant_servers_runtime.last_restart_reason)
	`, r.TenantID, r.ServerID, envText, boolToInt(r.Enabled), lastRestart, r.LastRestartReason)
	if err != nil {
		return fmt.Errorf("sqlite: upsert server runtime %s/%s: %w", r.TenantID, r.ServerID, err)
	}
	return nil
}

func (s *serverRuntimeStore) Delete(ctx context.Context, tenantID, serverID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_servers_runtime WHERE tenant_id = ? AND server_id = ?`,
		tenantID, serverID)
	if err != nil {
		return fmt.Errorf("sqlite: delete server runtime %s/%s: %w", tenantID, serverID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *serverRuntimeStore) List(ctx context.Context, tenantID string) ([]*ifaces.ServerRuntimeRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		serverRuntimeSelect+`WHERE tenant_id = ? ORDER BY server_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list server runtime: %w", err)
	}
	defer rows.Close()
	var out []*ifaces.ServerRuntimeRecord
	for rows.Next() {
		r, err := scanServerRuntime(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *serverRuntimeStore) RecordRestart(ctx context.Context, tenantID, serverID, reason string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	// Upsert via INSERT-or-UPDATE so the row exists even if no overrides
	// have been written.
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_servers_runtime
		    (tenant_id, server_id, env_overrides, enabled, last_restart_at, last_restart_reason)
		VALUES (?, ?, '{}', 1, ?, ?)
		ON CONFLICT(tenant_id, server_id) DO UPDATE SET
		    last_restart_at     = excluded.last_restart_at,
		    last_restart_reason = excluded.last_restart_reason
	`, tenantID, serverID, at.UTC().Format("2006-01-02T15:04:05.000Z"), reason)
	if err != nil {
		return fmt.Errorf("sqlite: record restart %s/%s: %w", tenantID, serverID, err)
	}
	return nil
}

func scanServerRuntime(rs rowScanner) (*ifaces.ServerRuntimeRecord, error) {
	var (
		r        ifaces.ServerRuntimeRecord
		envText  string
		enabled  int
		lastRA   string
		lastRRsn string
	)
	if err := rs.Scan(&r.TenantID, &r.ServerID, &envText, &enabled, &lastRA, &lastRRsn); err != nil {
		return nil, err
	}
	r.EnvOverrides = []byte(envText)
	r.Enabled = enabled != 0
	if lastRA != "" {
		r.LastRestartAt, _ = parseSQLiteTime(lastRA)
	}
	r.LastRestartReason = lastRRsn
	return &r, nil
}
