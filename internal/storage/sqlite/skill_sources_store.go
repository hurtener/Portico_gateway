// SQLite implementation of the Phase 8 skill-source repository.
// Tenant-scoped per CLAUDE.md §6.

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type skillSourceStore struct {
	db *sql.DB
}

func (s *skillSourceStore) Upsert(ctx context.Context, r *ifaces.SkillSourceRecord) error {
	if r == nil {
		return errors.New("sqlite: nil skill source record")
	}
	if r.TenantID == "" || r.Name == "" || r.Driver == "" {
		return errors.New("sqlite: tenant_id, name, driver required")
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	r.UpdatedAt = time.Now().UTC()
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if r.RefreshSeconds <= 0 {
		r.RefreshSeconds = 300
	}
	if r.Priority <= 0 {
		r.Priority = 100
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_skill_sources(
			tenant_id, name, driver, config_json, credential_ref,
			refresh_seconds, priority, enabled, created_at, updated_at,
			last_refresh_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, name) DO UPDATE SET
			driver           = excluded.driver,
			config_json      = excluded.config_json,
			credential_ref   = excluded.credential_ref,
			refresh_seconds  = excluded.refresh_seconds,
			priority         = excluded.priority,
			enabled          = excluded.enabled,
			updated_at       = excluded.updated_at
	`,
		r.TenantID, r.Name, r.Driver, string(r.ConfigJSON), nullableString(r.CredentialRef),
		r.RefreshSeconds, r.Priority, enabled,
		r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano),
		nullableTime(r.LastRefreshAt), nullableString(r.LastError))
	if err != nil {
		return fmt.Errorf("sqlite: upsert tenant_skill_sources: %w", err)
	}
	return nil
}

func (s *skillSourceStore) Get(ctx context.Context, tenantID, name string) (*ifaces.SkillSourceRecord, error) {
	if tenantID == "" || name == "" {
		return nil, errors.New("sqlite: tenant_id and name required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, name, driver, config_json, COALESCE(credential_ref,''),
		       refresh_seconds, priority, enabled, created_at, updated_at,
		       COALESCE(last_refresh_at,''), COALESCE(last_error,'')
		FROM tenant_skill_sources WHERE tenant_id = ? AND name = ?
	`, tenantID, name)
	r, err := scanSkillSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrNotFound
	}
	return r, err
}

func (s *skillSourceStore) List(ctx context.Context, tenantID string) ([]*ifaces.SkillSourceRecord, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: tenant_id required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, name, driver, config_json, COALESCE(credential_ref,''),
		       refresh_seconds, priority, enabled, created_at, updated_at,
		       COALESCE(last_refresh_at,''), COALESCE(last_error,'')
		FROM tenant_skill_sources WHERE tenant_id = ? ORDER BY priority ASC, name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tenant_skill_sources: %w", err)
	}
	defer rows.Close()
	var out []*ifaces.SkillSourceRecord
	for rows.Next() {
		r, err := scanSkillSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *skillSourceStore) Delete(ctx context.Context, tenantID, name string) error {
	if tenantID == "" || name == "" {
		return errors.New("sqlite: tenant_id and name required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_skill_sources WHERE tenant_id = ? AND name = ?`,
		tenantID, name)
	if err != nil {
		return fmt.Errorf("sqlite: delete tenant_skill_sources: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *skillSourceStore) MarkRefreshed(ctx context.Context, tenantID, name string, when time.Time, errStr string) error {
	if tenantID == "" || name == "" {
		return errors.New("sqlite: tenant_id and name required")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE tenant_skill_sources
		SET last_refresh_at = ?, last_error = ?, updated_at = ?
		WHERE tenant_id = ? AND name = ?
	`, when.Format(time.RFC3339Nano), nullableString(errStr), time.Now().UTC().Format(time.RFC3339Nano),
		tenantID, name)
	if err != nil {
		return fmt.Errorf("sqlite: mark_refreshed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

func scanSkillSource(r scanner) (*ifaces.SkillSourceRecord, error) {
	var rec ifaces.SkillSourceRecord
	var enabled int
	var configJSON, credRef, createdAt, updatedAt, lastRefreshAt, lastError string
	if err := r.Scan(
		&rec.TenantID, &rec.Name, &rec.Driver, &configJSON, &credRef,
		&rec.RefreshSeconds, &rec.Priority, &enabled,
		&createdAt, &updatedAt, &lastRefreshAt, &lastError,
	); err != nil {
		return nil, err
	}
	rec.ConfigJSON = []byte(configJSON)
	rec.CredentialRef = credRef
	rec.Enabled = enabled == 1
	rec.CreatedAt, _ = parseSQLiteTime(createdAt)
	rec.UpdatedAt, _ = parseSQLiteTime(updatedAt)
	if lastRefreshAt != "" {
		t, _ := parseSQLiteTime(lastRefreshAt)
		if !t.IsZero() {
			rec.LastRefreshAt = &t
		}
	}
	rec.LastError = lastError
	return &rec, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}
