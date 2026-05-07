// Package sqlite is the SQLite-backed implementation of the storage protocol.
//
// Driver: modernc.org/sqlite (pure Go, no CGo).
// Schema: forward-only migrations under ./migrations/.
//
// Self-registers with internal/storage at init() time. Consumers obtain a
// Backend by calling storage.Open(...) — they never import this package
// directly except for the blank-import side-effect at the binary entry point.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	// Pure-Go SQLite driver. Registers as "sqlite".
	_ "modernc.org/sqlite"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// DriverName is the driver string consumers select via config.Storage.Driver.
const DriverName = "sqlite"

func init() {
	storage.Register(DriverName, factory)
}

func factory(ctx context.Context, cfg config.StorageConfig, log *slog.Logger) (ifaces.Backend, error) {
	return Open(ctx, cfg.DSN, log)
}

// DB wraps *sql.DB and exposes per-table store implementations.
type DB struct {
	sql *sql.DB
	log *slog.Logger
}

// Open establishes a connection to the SQLite file at dsn, runs migrations,
// and returns a ready-to-use DB. dsn is passed to the driver verbatim
// (e.g. "file:./portico.db?cache=shared" or ":memory:").
func Open(ctx context.Context, dsn string, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.Default()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", dsn, err)
	}
	// modernc.org/sqlite runs single-connection well; cap to 1 to avoid
	// "database is locked" under concurrency in writers.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}

	d := &DB{sql: db, log: log}
	if err := d.runMigrations(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return d, nil
}

// Close releases the underlying connection pool.
func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

// SQL returns the underlying *sql.DB. Tests use this; production code goes
// through the typed store methods.
func (d *DB) SQL() *sql.DB { return d.sql }

// Tenants returns a TenantStore backed by this DB.
func (d *DB) Tenants() ifaces.TenantStore {
	return &tenantStore{db: d.sql}
}

// Audit returns an AuditStore backed by this DB.
func (d *DB) Audit() ifaces.AuditStore {
	return &auditStore{db: d.sql}
}

// Registry returns a RegistryStore backed by this DB.
func (d *DB) Registry() ifaces.RegistryStore {
	return &registryStore{db: d.sql}
}

// Skills returns a SkillEnablementStore backed by this DB.
func (d *DB) Skills() ifaces.SkillEnablementStore {
	return &skillStore{db: d.sql}
}

// Approvals returns an ApprovalStore backed by this DB.
func (d *DB) Approvals() ifaces.ApprovalStore {
	return &approvalStore{db: d.sql}
}

// Snapshots returns a SnapshotStore backed by this DB.
func (d *DB) Snapshots() ifaces.SnapshotStore {
	return &snapshotStore{db: d.sql}
}

// SkillSources returns a SkillSourceStore backed by this DB.
func (d *DB) SkillSources() ifaces.SkillSourceStore {
	return &skillSourceStore{db: d.sql}
}

// AuthoredSkills returns an AuthoredSkillStore backed by this DB.
func (d *DB) AuthoredSkills() ifaces.AuthoredSkillStore {
	return &authoredSkillStore{db: d.sql}
}

// PolicyRules returns a PolicyRulesStore backed by this DB (Phase 9).
func (d *DB) PolicyRules() ifaces.PolicyRulesStore {
	return &policyRulesStore{db: d.sql}
}

// ServerRuntime returns a ServerRuntimeStore backed by this DB (Phase 9).
func (d *DB) ServerRuntime() ifaces.ServerRuntimeStore {
	return &serverRuntimeStore{db: d.sql}
}

// EntityActivity returns an EntityActivityStore backed by this DB (Phase 9).
func (d *DB) EntityActivity() ifaces.EntityActivityStore {
	return &entityActivityStore{db: d.sql}
}

// Health pings the connection.
func (d *DB) Health(ctx context.Context) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("sqlite: not open")
	}
	return d.sql.PingContext(ctx)
}

// Driver implements ifaces.Backend.
func (d *DB) Driver() string { return DriverName }
