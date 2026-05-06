package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type skillStore struct {
	db *sql.DB
}

// skillEnabledAtArg returns the SQLite-compatible argument for the
// enabled_at column. Zero time → nil so the column DEFAULT (current
// timestamp) fires. Reuses tenant_store.go's nullTime layout.
func skillEnabledAtArg(t time.Time) any {
	return nullTime(t)
}

// skillEnabledAtParse parses the stored timestamp; zero time on parse
// failure (rows from earlier migrations may carry slightly different
// formatting).
func skillEnabledAtParse(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := parseSQLiteTime(s); err == nil {
		return t
	}
	return time.Time{}
}

// Set inserts or updates the row identified by (tenant, session, skill).
// session_id is stored as empty string for tenant-wide rules per the
// schema in 0001_init.sql.
func (s *skillStore) Set(ctx context.Context, e *ifaces.SkillEnablement) error {
	if e == nil {
		return errors.New("sqlite: nil skill enablement")
	}
	if e.TenantID == "" || e.SkillID == "" {
		return errors.New("sqlite: tenant_id and skill_id are required")
	}
	enabled := 0
	if e.Enabled {
		enabled = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO skill_enablement(tenant_id, session_id, skill_id, enabled, enabled_at)
		VALUES (?, ?, ?, ?, COALESCE(?, strftime('%Y-%m-%dT%H:%M:%fZ','now')))
		ON CONFLICT(tenant_id, session_id, skill_id) DO UPDATE SET
			enabled    = excluded.enabled,
			enabled_at = excluded.enabled_at
	`,
		e.TenantID, e.SessionID, e.SkillID, enabled, skillEnabledAtArg(e.EnabledAt))
	if err != nil {
		return fmt.Errorf("sqlite: upsert skill_enablement: %w", err)
	}
	return nil
}

func (s *skillStore) Delete(ctx context.Context, tenantID, sessionID, skillID string) error {
	if tenantID == "" || skillID == "" {
		return errors.New("sqlite: tenant_id and skill_id are required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM skill_enablement WHERE tenant_id = ? AND session_id = ? AND skill_id = ?`,
		tenantID, sessionID, skillID)
	if err != nil {
		return fmt.Errorf("sqlite: delete skill_enablement: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrNotFound
	}
	return nil
}

// Resolve answers per-session > per-tenant > not-found. Returns
// (enabled=false, found=false, nil) when no row matches; the caller
// then falls back to the manifest default.
func (s *skillStore) Resolve(ctx context.Context, tenantID, sessionID, skillID string) (bool, bool, error) {
	if tenantID == "" || skillID == "" {
		return false, false, errors.New("sqlite: tenant_id and skill_id are required")
	}
	if sessionID != "" {
		var en int
		err := s.db.QueryRowContext(ctx,
			`SELECT enabled FROM skill_enablement WHERE tenant_id = ? AND session_id = ? AND skill_id = ?`,
			tenantID, sessionID, skillID).Scan(&en)
		if err == nil {
			return en == 1, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, false, fmt.Errorf("sqlite: resolve skill_enablement (session): %w", err)
		}
	}
	var en int
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled FROM skill_enablement WHERE tenant_id = ? AND session_id = '' AND skill_id = ?`,
		tenantID, skillID).Scan(&en)
	if err == nil {
		return en == 1, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, false, fmt.Errorf("sqlite: resolve skill_enablement (tenant): %w", err)
	}
	return false, false, nil
}

func (s *skillStore) ListForSession(ctx context.Context, tenantID, sessionID string) ([]*ifaces.SkillEnablement, error) {
	if tenantID == "" || sessionID == "" {
		return nil, errors.New("sqlite: tenant_id and session_id are required")
	}
	return s.list(ctx,
		`SELECT tenant_id, session_id, skill_id, enabled, enabled_at FROM skill_enablement
		 WHERE tenant_id = ? AND session_id = ? ORDER BY skill_id`,
		tenantID, sessionID)
}

func (s *skillStore) ListForTenant(ctx context.Context, tenantID string) ([]*ifaces.SkillEnablement, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: tenant_id is required")
	}
	return s.list(ctx,
		`SELECT tenant_id, session_id, skill_id, enabled, enabled_at FROM skill_enablement
		 WHERE tenant_id = ? AND session_id = '' ORDER BY skill_id`,
		tenantID)
}

func (s *skillStore) list(ctx context.Context, q string, args ...any) ([]*ifaces.SkillEnablement, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: query skill_enablement: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.SkillEnablement, 0)
	for rows.Next() {
		var e ifaces.SkillEnablement
		var enabled int
		var enabledAt string
		if err := rows.Scan(&e.TenantID, &e.SessionID, &e.SkillID, &enabled, &enabledAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan skill_enablement: %w", err)
		}
		e.Enabled = enabled == 1
		e.EnabledAt = skillEnabledAtParse(enabledAt)
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: rows skill_enablement: %w", err)
	}
	return out, nil
}
