-- Phase 6: snapshot fingerprints + drift fingerprint cache.
--
-- Adds the overall canonical hash to catalog_snapshots so drift comparisons
-- can short-circuit on top-level equality. Adds schema_fingerprints as a
-- (tenant, server) → recent hashes table the drift detector consults to
-- decide whether a re-fingerprint constitutes drift.

ALTER TABLE catalog_snapshots ADD COLUMN overall_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_snapshots_session ON catalog_snapshots(session_id, created_at DESC);

CREATE TABLE IF NOT EXISTS schema_fingerprints (
    tenant_id   TEXT NOT NULL,
    server_id   TEXT NOT NULL,
    seen_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    hash        TEXT NOT NULL,
    tools_count INTEGER NOT NULL,
    PRIMARY KEY (tenant_id, server_id, hash)
);

CREATE INDEX IF NOT EXISTS idx_fingerprints_server
    ON schema_fingerprints(tenant_id, server_id, seen_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (3);
