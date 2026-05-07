-- Phase 8: skill sources first-class + in-Portico authored skills.
-- All tables are tenant-scoped per CLAUDE.md §6.

CREATE TABLE IF NOT EXISTS tenant_skill_sources (
    tenant_id        TEXT NOT NULL,
    name             TEXT NOT NULL,            -- operator-chosen handle, unique per tenant
    driver           TEXT NOT NULL,            -- 'git' | 'http' | 'localdir' | 'authored'
    config_json      TEXT NOT NULL,            -- driver-specific config (URL, branch, feed URL, …)
    credential_ref   TEXT,                     -- vault key for creds, NULL for public sources
    refresh_seconds  INTEGER NOT NULL DEFAULT 300,
    priority         INTEGER NOT NULL DEFAULT 100,  -- lower wins on collision
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    last_refresh_at  TEXT,
    last_error       TEXT,
    PRIMARY KEY (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_tenant_skill_sources_driver
    ON tenant_skill_sources(tenant_id, driver);

-- Authored skills are stored as a (manifest, files) tuple per tenant.
CREATE TABLE IF NOT EXISTS tenant_authored_skills (
    tenant_id        TEXT NOT NULL,
    skill_id         TEXT NOT NULL,            -- pack id from the manifest
    version          TEXT NOT NULL,            -- "1.2.0" or "1.2.0-rc1"
    status           TEXT NOT NULL,            -- 'draft' | 'published' | 'archived'
    manifest_json    TEXT NOT NULL,            -- canonical manifest
    checksum         TEXT NOT NULL,            -- SHA-256 of canonical(manifest + files)
    author_user_id   TEXT,
    created_at       TEXT NOT NULL,
    published_at     TEXT,
    archived_at      TEXT,
    PRIMARY KEY (tenant_id, skill_id, version)
);

CREATE INDEX IF NOT EXISTS idx_tenant_authored_skills_status
    ON tenant_authored_skills(tenant_id, status, published_at DESC);

-- Files belonging to an authored skill (SKILL.md, prompts, resources, optional ui://).
CREATE TABLE IF NOT EXISTS tenant_authored_skill_files (
    tenant_id        TEXT NOT NULL,
    skill_id         TEXT NOT NULL,
    version          TEXT NOT NULL,
    relpath          TEXT NOT NULL,            -- 'SKILL.md', 'prompts/triage.md', 'apps/console.html'
    mime_type        TEXT NOT NULL,
    contents         BLOB NOT NULL,
    PRIMARY KEY (tenant_id, skill_id, version, relpath),
    FOREIGN KEY (tenant_id, skill_id, version)
        REFERENCES tenant_authored_skills(tenant_id, skill_id, version)
        ON DELETE CASCADE
);

-- Pointer to the active version per (tenant, skill_id). Updated on publish.
CREATE TABLE IF NOT EXISTS tenant_authored_active_skill (
    tenant_id        TEXT NOT NULL,
    skill_id         TEXT NOT NULL,
    active_version   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, skill_id),
    FOREIGN KEY (tenant_id, skill_id, active_version)
        REFERENCES tenant_authored_skills(tenant_id, skill_id, version)
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (8);
