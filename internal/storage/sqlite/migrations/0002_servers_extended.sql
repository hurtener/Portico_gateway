-- Phase 2: extend the servers table with runtime/transport state and add the
-- server_instances table for supervisor bookkeeping.

ALTER TABLE servers ADD COLUMN runtime_mode TEXT NOT NULL DEFAULT 'shared_global';
ALTER TABLE servers ADD COLUMN transport TEXT NOT NULL DEFAULT 'stdio';
ALTER TABLE servers ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE servers ADD COLUMN status_detail TEXT;

CREATE TABLE IF NOT EXISTS server_instances (
    id            TEXT PRIMARY KEY,                    -- ULID-like
    tenant_id     TEXT NOT NULL,
    server_id     TEXT NOT NULL,
    user_id       TEXT NOT NULL DEFAULT '',
    session_id    TEXT NOT NULL DEFAULT '',
    pid           INTEGER NOT NULL DEFAULT 0,
    started_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_call_at  TEXT,
    state         TEXT NOT NULL,
    restart_count INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    schema_hash   TEXT,
    FOREIGN KEY (tenant_id, server_id) REFERENCES servers(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_instances_tenant_server ON server_instances(tenant_id, server_id);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);
