-- Phase 15.5 — Governance entities: Customers, Teams, Virtual Keys.
-- All tenant-scoped (tenant_id NOT NULL, PK leads with tenant_id). VK secrets
-- are NEVER stored: only salt + HMAC-SHA256(salt, secret). profile_id is the
-- reserved Phase 14 Agent Profile linkage (VK ∩ Profile intersect at runtime).
CREATE TABLE IF NOT EXISTS governance_customers (
    tenant_id   TEXT NOT NULL,
    id          TEXT NOT NULL,                -- ULID
    name        TEXT NOT NULL,
    description TEXT,
    webhook_url TEXT,                          -- optional budget-critical webhook
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS governance_teams (
    tenant_id    TEXT NOT NULL,
    id           TEXT NOT NULL,               -- ULID
    customer_id  TEXT,                         -- nullable; team may stand alone under tenant
    name         TEXT NOT NULL,
    description  TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name),
    -- Plain composite FK (no ON DELETE SET NULL: a composite SET NULL would also
    -- null tenant_id, which is NOT NULL -> constraint failure). DeleteCustomer
    -- nulls child teams' customer_id in-app within one transaction before
    -- removing the customer.
    FOREIGN KEY (tenant_id, customer_id) REFERENCES governance_customers(tenant_id, id)
);

CREATE TABLE IF NOT EXISTS governance_virtual_keys (
    tenant_id    TEXT NOT NULL,
    id           TEXT NOT NULL,               -- ULID (globally unique; resolver looks up by id)
    name         TEXT NOT NULL,
    salt         BLOB NOT NULL,
    hmac         BLOB NOT NULL,                -- HMAC-SHA256(salt, secret); secret never stored
    parent_kind  TEXT NOT NULL DEFAULT 'none' CHECK (parent_kind IN ('none','team','customer')),
    parent_id    TEXT,                         -- team id or customer id when parent_kind != 'none'
    profile_id   TEXT,                         -- reserved Phase 14 Agent Profile FK; nullable
    scopes       TEXT NOT NULL DEFAULT '[]',   -- JSON array
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL,
    rotated_at   TEXT,
    revoked_at   TEXT,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE SET NULL
);
-- The resolver looks up a presented VK by its globally-unique id (auth boundary,
-- analogous to JWT->tenant resolution); index id for O(1) lookup.
CREATE INDEX IF NOT EXISTS idx_vk_id ON governance_virtual_keys(id);

CREATE TABLE IF NOT EXISTS vk_provider_allowlist (
    tenant_id       TEXT NOT NULL,
    vk_id           TEXT NOT NULL,
    provider_driver TEXT NOT NULL,            -- e.g. 'anthropic', 'custom_openai'
    PRIMARY KEY (tenant_id, vk_id, provider_driver),
    FOREIGN KEY (tenant_id, vk_id) REFERENCES governance_virtual_keys(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS vk_model_allowlist (
    tenant_id TEXT NOT NULL,
    vk_id     TEXT NOT NULL,
    alias     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, vk_id, alias),
    FOREIGN KEY (tenant_id, vk_id) REFERENCES governance_virtual_keys(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS vk_mcp_server_allowlist (
    tenant_id TEXT NOT NULL,
    vk_id     TEXT NOT NULL,
    server_id TEXT NOT NULL,
    PRIMARY KEY (tenant_id, vk_id, server_id),
    FOREIGN KEY (tenant_id, vk_id) REFERENCES governance_virtual_keys(tenant_id, id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (21);
