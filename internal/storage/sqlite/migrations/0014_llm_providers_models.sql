-- Phase 13 unit 1: per-tenant LLM provider registry + weighted keys.
CREATE TABLE IF NOT EXISTS tenant_llm_providers (
    tenant_id      TEXT NOT NULL,
    name           TEXT NOT NULL,
    driver         TEXT NOT NULL,            -- a Bifrost native driver name OR 'custom_openai'
    config_json    TEXT NOT NULL DEFAULT '{}', -- driver-specific (endpoint, region, org_id, base_url, headers, …)
    credential_ref TEXT,                      -- default vault key (multi-key cases live in the keys table)
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS tenant_llm_provider_keys (
    tenant_id       TEXT NOT NULL,
    provider_name   TEXT NOT NULL,
    key_id          TEXT NOT NULL,            -- ULID
    credential_ref  TEXT NOT NULL,            -- vault entry holding the secret
    weight          REAL NOT NULL DEFAULT 1.0,
    model_allowlist TEXT NOT NULL DEFAULT '[]', -- JSON array; empty = all models
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    PRIMARY KEY (tenant_id, provider_name, key_id),
    FOREIGN KEY (tenant_id, provider_name) REFERENCES tenant_llm_providers(tenant_id, name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_llm_provider_keys_provider
    ON tenant_llm_provider_keys(tenant_id, provider_name);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (14);
