package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ErrNotFound is the package-local alias for the canonical ifaces.ErrNotFound.
// Kept for back-compat within the package; new code should import ifaces.
var ErrNotFound = ifaces.ErrNotFound

type tenantStore struct {
	db *sql.DB
}

// tenantSelect selects every column we currently care about. Kept as a const
// so Get + List share the layout (and any extension changes one place).
const tenantSelect = `
	SELECT id, display_name, plan,
	       runtime_mode, max_concurrent_sessions, max_requests_per_minute,
	       audit_retention_days, jwt_issuer, jwt_jwks_url, status,
	       created_at, updated_at
	FROM tenants
`

func (s *tenantStore) Get(ctx context.Context, id string) (*ifaces.Tenant, error) {
	row := s.db.QueryRowContext(ctx, tenantSelect+`WHERE id = ?`, id)
	t, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *tenantStore) List(ctx context.Context) ([]*ifaces.Tenant, error) {
	rows, err := s.db.QueryContext(ctx, tenantSelect+`ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list tenants: %w", err)
	}
	defer rows.Close()

	var out []*ifaces.Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *tenantStore) Upsert(ctx context.Context, t *ifaces.Tenant) error {
	if t == nil || t.ID == "" {
		return errors.New("sqlite: tenant id is required")
	}
	// Apply defaults for fields that are non-nullable but unset by the caller
	// — keep parity with the migration-supplied DEFAULTs.
	runtimeMode := t.RuntimeMode
	if runtimeMode == "" {
		runtimeMode = "shared_global"
	}
	status := t.Status
	if status == "" {
		status = "active"
	}
	maxConcurrent := t.MaxConcurrentSessions
	if maxConcurrent == 0 {
		maxConcurrent = 16
	}
	maxRPM := t.MaxRequestsPerMinute
	if maxRPM == 0 {
		maxRPM = 600
	}
	retention := t.AuditRetentionDays
	if retention == 0 {
		retention = 30
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenants(id, display_name, plan,
		                    runtime_mode, max_concurrent_sessions, max_requests_per_minute,
		                    audit_retention_days, jwt_issuer, jwt_jwks_url, status,
		                    created_at, updated_at)
		VALUES (?, ?, ?,
		        ?, ?, ?,
		        ?, ?, ?, ?,
		        COALESCE(?, strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		        strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(id) DO UPDATE SET
		    display_name             = excluded.display_name,
		    plan                     = excluded.plan,
		    runtime_mode             = excluded.runtime_mode,
		    max_concurrent_sessions  = excluded.max_concurrent_sessions,
		    max_requests_per_minute  = excluded.max_requests_per_minute,
		    audit_retention_days     = excluded.audit_retention_days,
		    jwt_issuer               = excluded.jwt_issuer,
		    jwt_jwks_url             = excluded.jwt_jwks_url,
		    status                   = excluded.status,
		    updated_at               = strftime('%Y-%m-%dT%H:%M:%fZ','now')
	`,
		t.ID, t.DisplayName, t.Plan,
		runtimeMode, maxConcurrent, maxRPM,
		retention, t.JWTIssuer, t.JWTJWKSURL, status,
		nullTime(t.CreatedAt))
	if err != nil {
		return fmt.Errorf("sqlite: upsert tenant %q: %w", t.ID, err)
	}
	return nil
}

func (s *tenantStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete tenant %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// rowScanner is the common surface of *sql.Row and *sql.Rows so scanTenant
// can serve both Get and List paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTenant(rs rowScanner) (*ifaces.Tenant, error) {
	var (
		t                    ifaces.Tenant
		createdAt, updatedAt string
	)
	if err := rs.Scan(
		&t.ID, &t.DisplayName, &t.Plan,
		&t.RuntimeMode, &t.MaxConcurrentSessions, &t.MaxRequestsPerMinute,
		&t.AuditRetentionDays, &t.JWTIssuer, &t.JWTJWKSURL, &t.Status,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	t.CreatedAt, _ = parseSQLiteTime(createdAt)
	t.UpdatedAt, _ = parseSQLiteTime(updatedAt)
	return &t, nil
}

// nullTime returns the zero time as nil so SQLite uses its DEFAULT clause.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// parseSQLiteTime parses our default ISO format with millisecond precision.
func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", s)
}
