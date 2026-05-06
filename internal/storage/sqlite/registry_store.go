package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type registryStore struct {
	db *sql.DB
}

func (s *registryStore) GetServer(ctx context.Context, tenantID, id string) (*ifaces.ServerRecord, error) {
	if tenantID == "" || id == "" {
		return nil, errors.New("sqlite: tenant_id and id are required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, id, display_name, transport, runtime_mode,
		       spec_json, enabled, COALESCE(status,'unknown'),
		       COALESCE(status_detail,''), COALESCE(schema_hash,''),
		       COALESCE(last_error,''), created_at, updated_at
		FROM servers WHERE tenant_id = ? AND id = ?
	`, tenantID, id)
	r, err := scanServer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	return r, err
}

func (s *registryStore) ListServers(ctx context.Context, tenantID string) ([]*ifaces.ServerRecord, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: tenant_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, id, display_name, transport, runtime_mode,
		       spec_json, enabled, COALESCE(status,'unknown'),
		       COALESCE(status_detail,''), COALESCE(schema_hash,''),
		       COALESCE(last_error,''), created_at, updated_at
		FROM servers WHERE tenant_id = ? ORDER BY id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list servers: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.ServerRecord
	for rows.Next() {
		r, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *registryStore) UpsertServer(ctx context.Context, r *ifaces.ServerRecord) error {
	if r == nil || r.TenantID == "" || r.ID == "" {
		return errors.New("sqlite: server record requires tenant_id and id")
	}
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO servers(
			tenant_id, id, display_name, transport, runtime_mode,
			spec_json, enabled, status, status_detail, schema_hash, last_error,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			COALESCE(?, strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(tenant_id, id) DO UPDATE SET
			display_name = excluded.display_name,
			transport = excluded.transport,
			runtime_mode = excluded.runtime_mode,
			spec_json = excluded.spec_json,
			enabled = excluded.enabled,
			status = excluded.status,
			status_detail = excluded.status_detail,
			schema_hash = excluded.schema_hash,
			last_error = excluded.last_error,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
	`, r.TenantID, r.ID, r.DisplayName, r.Transport, r.RuntimeMode,
		string(r.Spec), enabled, r.Status, r.StatusDetail, r.SchemaHash, r.LastError,
		nullTime(r.CreatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: upsert server %q/%q: %w", r.TenantID, r.ID, err)
	}
	return nil
}

func (s *registryStore) DeleteServer(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: tenant_id and id are required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE tenant_id = ? AND id = ?`, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete server %q/%q: %w", tenantID, id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *registryStore) UpdateServerStatus(ctx context.Context, tenantID, id, status, detail string) error {
	if tenantID == "" || id == "" {
		return errors.New("sqlite: tenant_id and id are required")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE servers
		SET status = ?, status_detail = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE tenant_id = ? AND id = ?
	`, status, detail, tenantID, id)
	if err != nil {
		return fmt.Errorf("sqlite: update server status %q/%q: %w", tenantID, id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *registryStore) UpsertInstance(ctx context.Context, i *ifaces.InstanceRecord) error {
	if i == nil || i.ID == "" || i.TenantID == "" || i.ServerID == "" {
		return errors.New("sqlite: instance record requires id/tenant_id/server_id")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO server_instances(
			id, tenant_id, server_id, user_id, session_id, pid,
			started_at, last_call_at, state, restart_count, last_error, schema_hash
		)
		VALUES (?, ?, ?, ?, ?, ?,
			COALESCE(?, strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			pid = excluded.pid,
			last_call_at = excluded.last_call_at,
			state = excluded.state,
			restart_count = excluded.restart_count,
			last_error = excluded.last_error,
			schema_hash = excluded.schema_hash
	`, i.ID, i.TenantID, i.ServerID, i.UserID, i.SessionID, i.PID,
		nullTime(i.StartedAt), nullTime(i.LastCallAt),
		i.State, i.RestartCount, i.LastError, i.SchemaHash)
	if err != nil {
		return fmt.Errorf("sqlite: upsert instance %q: %w", i.ID, err)
	}
	return nil
}

func (s *registryStore) DeleteInstance(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("sqlite: instance id is required")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM server_instances WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete instance %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *registryStore) ListInstances(ctx context.Context, tenantID, serverID string) ([]*ifaces.InstanceRecord, error) {
	if tenantID == "" || serverID == "" {
		return nil, errors.New("sqlite: tenant_id and server_id are required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, server_id, user_id, session_id, pid,
		       started_at, COALESCE(last_call_at,''), state, restart_count,
		       COALESCE(last_error,''), COALESCE(schema_hash,'')
		FROM server_instances
		WHERE tenant_id = ? AND server_id = ?
		ORDER BY started_at DESC
	`, tenantID, serverID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list instances: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.InstanceRecord
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func scanServer(rs rowScanner) (*ifaces.ServerRecord, error) {
	var (
		r                    ifaces.ServerRecord
		spec                 string
		enabled              int
		createdAt, updatedAt string
	)
	if err := rs.Scan(
		&r.TenantID, &r.ID, &r.DisplayName, &r.Transport, &r.RuntimeMode,
		&spec, &enabled, &r.Status, &r.StatusDetail, &r.SchemaHash, &r.LastError,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	r.Spec = []byte(spec)
	r.Enabled = enabled != 0
	r.CreatedAt, _ = parseSQLiteTime(createdAt)
	r.UpdatedAt, _ = parseSQLiteTime(updatedAt)
	return &r, nil
}

func scanInstance(rs rowScanner) (*ifaces.InstanceRecord, error) {
	var (
		i                       ifaces.InstanceRecord
		startedAt, lastCallAt   string
	)
	if err := rs.Scan(
		&i.ID, &i.TenantID, &i.ServerID, &i.UserID, &i.SessionID, &i.PID,
		&startedAt, &lastCallAt, &i.State, &i.RestartCount, &i.LastError, &i.SchemaHash,
	); err != nil {
		return nil, err
	}
	i.StartedAt, _ = parseSQLiteTime(startedAt)
	if lastCallAt != "" {
		i.LastCallAt, _ = parseSQLiteTime(lastCallAt)
	}
	return &i, nil
}

// Compile-time assertion that registryStore implements the iface.
var _ ifaces.RegistryStore = (*registryStore)(nil)

// avoid unused warning if scanInstance is exclusively used through the iface
var _ = time.Now
