-- Phase 15.5 — SQLite-backed semantic cache entries (dev/test driver only;
-- prod uses redis/weaviate/qdrant via the §4.4 cache seam). Per-tenant by PK.
CREATE TABLE IF NOT EXISTS llm_cache_entries (
    tenant_id  TEXT NOT NULL,
    cache_key  TEXT NOT NULL,
    mode       TEXT NOT NULL,                 -- 'exact'|'semantic'
    alias      TEXT NOT NULL,
    payload    BLOB NOT NULL,                 -- serialised response, redactor-applied upstream
    embedding  BLOB,                          -- semantic mode only
    similarity REAL,                          -- denormalised for analytics
    tokens     INTEGER NOT NULL DEFAULT 0,
    cost_usd   REAL NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, cache_key)
);
CREATE INDEX IF NOT EXISTS idx_cache_entries_expiry ON llm_cache_entries(tenant_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_cache_entries_alias ON llm_cache_entries(tenant_id, alias);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (23);
