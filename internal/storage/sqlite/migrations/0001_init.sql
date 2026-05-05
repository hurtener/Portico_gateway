-- Phase 0: full V1 baseline schema.
-- Tables that later phases populate are defined here so a single migration
-- materializes the V1 surface.

PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS tenants (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    plan         TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS servers (
    tenant_id   TEXT NOT NULL,
    id          TEXT NOT NULL,
    spec_json   TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    schema_hash TEXT,
    last_error  TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    user_id       TEXT,
    snapshot_id   TEXT,
    started_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ended_at      TEXT,
    metadata_json TEXT,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON sessions(tenant_id, started_at DESC);

CREATE TABLE IF NOT EXISTS catalog_snapshots (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    session_id   TEXT,
    payload_json TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_snapshots_tenant ON catalog_snapshots(tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS skill_enablement (
    tenant_id  TEXT NOT NULL,
    -- session_id is empty string for tenant-wide enablement; non-empty for per-session.
    -- We avoid NULL here so the composite primary key works without expressions.
    session_id TEXT NOT NULL DEFAULT '',
    skill_id   TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    enabled_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (tenant_id, session_id, skill_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS approvals (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    session_id    TEXT NOT NULL,
    user_id       TEXT,
    tool          TEXT NOT NULL,
    args_summary  TEXT,
    risk_class    TEXT,
    status        TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    decided_at    TEXT,
    expires_at    TEXT NOT NULL,
    metadata_json TEXT,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_approvals_tenant_status ON approvals(tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_events (
    id           TEXT PRIMARY KEY,
    tenant_id    TEXT NOT NULL,
    type         TEXT NOT NULL,
    session_id   TEXT,
    user_id      TEXT,
    occurred_at  TEXT NOT NULL,
    trace_id     TEXT,
    span_id      TEXT,
    payload_json TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_events(tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_type ON audit_events(tenant_id, type, occurred_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
