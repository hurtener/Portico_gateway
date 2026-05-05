package sqlite

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// runMigrations applies any pending migrations in order. Idempotent.
func (d *DB) runMigrations(ctx context.Context) error {
	// Bootstrap schema_migrations table; subsequent migrations may re-create it
	// (CREATE TABLE IF NOT EXISTS) but we need to be able to query it now.
	if _, err := d.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
		    version INTEGER PRIMARY KEY,
		    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);
	`); err != nil {
		return fmt.Errorf("sqlite: bootstrap schema_migrations: %w", err)
	}

	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("sqlite: read embedded migrations: %w", err)
	}

	type m struct {
		version int
		name    string
		body    []byte
	}
	var ms []m
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".sql") {
			continue
		}
		v, err := parseVersion(f.Name())
		if err != nil {
			return fmt.Errorf("sqlite: migration %q: %w", f.Name(), err)
		}
		body, err := migrationFS.ReadFile("migrations/" + f.Name())
		if err != nil {
			return fmt.Errorf("sqlite: read migration %q: %w", f.Name(), err)
		}
		ms = append(ms, m{version: v, name: f.Name(), body: body})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })

	for _, mi := range ms {
		var applied int
		err := d.sql.QueryRowContext(ctx,
			`SELECT 1 FROM schema_migrations WHERE version = ?`, mi.version).Scan(&applied)
		switch {
		case err == nil:
			// already applied
			d.log.Debug("migration already applied", "version", mi.version, "name", mi.name)
			continue
		case err.Error() == "sql: no rows in result set":
			// fall through and apply
		default:
			return fmt.Errorf("sqlite: check migration %d: %w", mi.version, err)
		}

		d.log.Info("applying migration", "version", mi.version, "name", mi.name)
		// Execute the whole file as one statement; modernc.org/sqlite supports
		// multi-statement input.
		if _, err := d.sql.ExecContext(ctx, string(mi.body)); err != nil {
			return fmt.Errorf("sqlite: apply migration %d (%s): %w", mi.version, mi.name, err)
		}
	}
	return nil
}

// parseVersion turns "0001_init.sql" -> 1.
func parseVersion(name string) (int, error) {
	base := name
	if i := strings.IndexByte(base, '_'); i > 0 {
		base = base[:i]
	} else if i := strings.IndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	v, err := strconv.Atoi(strings.TrimLeft(base, "0"))
	if err != nil && strings.TrimLeft(base, "0") == "" {
		// All zeros — treat as version 0
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return v, nil
}
