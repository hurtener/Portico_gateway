-- Phase 11: imported session bundles. When an operator imports a
-- bundle from another instance (or from this one), the bundle is
-- registered under a synthetic session id (`imported:<bundle_id>`)
-- that the inspector reads alongside live sessions but the runtime
-- refuses to write to.

CREATE TABLE IF NOT EXISTS imported_sessions (
    bundle_id        TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL,           -- tenant the bundle was imported INTO
    source_tenant_id TEXT NOT NULL,           -- tenant the bundle was exported FROM
    session_id       TEXT NOT NULL,           -- synthetic: 'imported:<bundle_id>'
    source_session_id TEXT NOT NULL,          -- session id from the source instance
    imported_at      TEXT NOT NULL,           -- RFC3339
    range_from       TEXT NOT NULL,
    range_to         TEXT NOT NULL,
    counts_json      TEXT NOT NULL DEFAULT '{}',
    -- The bundle payload lives inline so the inspector can render
    -- without re-parsing the tar.gz on every load. Stored as canonical
    -- JSONL concatenated by section, sha256-checksummed at import time.
    bundle_blob      BLOB NOT NULL,
    checksum         TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_imported_tenant_imported_at
    ON imported_sessions(tenant_id, imported_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (13);
