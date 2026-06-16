-- Phase 13 unit 2: per-tenant model aliases → (provider, provider_model).
CREATE TABLE IF NOT EXISTS tenant_llm_models (
    tenant_id           TEXT NOT NULL,
    alias               TEXT NOT NULL,            -- e.g. 'gpt-4', 'fast-summary'
    provider_name       TEXT NOT NULL,            -- references tenant_llm_providers(name)
    provider_model      TEXT NOT NULL,            -- e.g. 'gpt-4o', 'claude-3-5-sonnet-20241022'
    default_params_json TEXT NOT NULL DEFAULT '{}', -- temperature, top_p, max_tokens
    capabilities        TEXT NOT NULL DEFAULT '[]', -- JSON array: chat|completion|embedding|moderation|tool_use
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    PRIMARY KEY (tenant_id, alias),
    FOREIGN KEY (tenant_id, provider_name) REFERENCES tenant_llm_providers(tenant_id, name)
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (15);
