-- Phase 13 unit 9: LLM cost telemetry. Global price book + per-tenant daily rollup.
CREATE TABLE IF NOT EXISTS llm_unit_costs (
    provider_driver TEXT NOT NULL,         -- 'openai', 'anthropic', 'custom_openai', …
    provider_model  TEXT NOT NULL,
    input_per_1k    REAL NOT NULL,
    output_per_1k   REAL NOT NULL,
    PRIMARY KEY (provider_driver, provider_model)
);

CREATE TABLE IF NOT EXISTS tenant_llm_cost_daily (
    tenant_id   TEXT NOT NULL,
    day         TEXT NOT NULL,             -- YYYY-MM-DD UTC
    alias       TEXT NOT NULL,
    requests    INTEGER NOT NULL DEFAULT 0,
    input_tok   INTEGER NOT NULL DEFAULT 0,
    output_tok  INTEGER NOT NULL DEFAULT 0,
    cost_usd    REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day, alias)
);
CREATE INDEX IF NOT EXISTS idx_llm_cost_daily ON tenant_llm_cost_daily(tenant_id, day DESC, alias);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (17);
