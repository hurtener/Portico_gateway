-- Phase 13 unit 8a: per-tenant LLM quota limits (one row per tenant).
CREATE TABLE IF NOT EXISTS tenant_llm_quotas (
    tenant_id           TEXT NOT NULL,
    requests_per_minute INTEGER NOT NULL DEFAULT 600,
    tokens_per_minute   INTEGER NOT NULL DEFAULT 200000,
    tokens_per_day      INTEGER NOT NULL DEFAULT 4000000,
    cost_usd_per_day    REAL    NOT NULL DEFAULT 100.0,
    updated_at          TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id)
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (16);
