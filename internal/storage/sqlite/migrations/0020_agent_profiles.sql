-- Phase 14 — Agent Profiles.
--
-- agent_profiles is the tenant-scoped consumer-binding primitive: which MCP
-- servers/tools, Skill Packs, and LLM aliases a logical agent may use, plus
-- its scope set. The four allowlist slices are loaded from their join tables.
-- Timestamps are RFC3339 UTC strings.
CREATE TABLE IF NOT EXISTS agent_profiles (
    tenant_id          TEXT NOT NULL,
    id                 TEXT NOT NULL,                -- ULID
    name               TEXT NOT NULL,
    description        TEXT,
    scopes             TEXT NOT NULL DEFAULT '[]',   -- JSON array
    policy_bundle_ref  TEXT,
    parent_profile_id  TEXT,                         -- reserved; nullable; no FK (inheritance is post-V2)
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS agent_profile_mcp_servers (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    server_name TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, server_name),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_tools (
    tenant_id     TEXT NOT NULL,
    profile_id    TEXT NOT NULL,
    namespaced_id TEXT NOT NULL,                     -- "github.list_issues"
    PRIMARY KEY (tenant_id, profile_id, namespaced_id),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_skills (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    skill_id    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, skill_id),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_models (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    alias       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, alias),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_jwt_bindings (
    tenant_id   TEXT NOT NULL,
    jwt_sub     TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, jwt_sub),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (20);
