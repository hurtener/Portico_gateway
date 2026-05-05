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

func (s *tenantStore) Get(ctx context.Context, id string) (*ifaces.Tenant, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, display_name, plan, created_at, updated_at
		FROM tenants WHERE id = ?
	`, id)
	t, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *tenantStore) List(ctx context.Context) ([]*ifaces.Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, display_name, plan, created_at, updated_at
		FROM tenants ORDER BY id
	`)
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
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenants(id, display_name, plan, created_at, updated_at)
		VALUES (?, ?, ?, COALESCE(?, strftime('%Y-%m-%dT%H:%M:%fZ','now')), strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(id) DO UPDATE SET
		    display_name = excluded.display_name,
		    plan = excluded.plan,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
	`, t.ID, t.DisplayName, t.Plan, nullTime(t.CreatedAt))
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
		t                       ifaces.Tenant
		createdAt, updatedAt    string
	)
	if err := rs.Scan(&t.ID, &t.DisplayName, &t.Plan, &createdAt, &updatedAt); err != nil {
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
