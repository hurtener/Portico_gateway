-- Phase 9: Console CRUD — extend tenants with runtime fields and add three
-- new tables for policy rules, server runtime overrides, and entity-scoped
-- activity projection. Forward-only.

-- 1) Tenants: extra runtime configuration columns. ALTER TABLE ADD COLUMN
--    is forward-only and SQLite handles defaults inline.
ALTER TABLE tenants ADD COLUMN runtime_mode TEXT NOT NULL DEFAULT 'shared_global';
ALTER TABLE tenants ADD COLUMN max_concurrent_sessions INTEGER NOT NULL DEFAULT 16;
ALTER TABLE tenants ADD COLUMN max_requests_per_minute INTEGER NOT NULL DEFAULT 600;
ALTER TABLE tenants ADD COLUMN audit_retention_days INTEGER NOT NULL DEFAULT 30;
ALTER TABLE tenants ADD COLUMN jwt_issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN jwt_jwks_url TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN status TEXT NOT NULL DEFAULT 'active';

-- 2) Policy rules: one row per rule, ordered by priority.
CREATE TABLE IF NOT EXISTS tenant_policy_rules (
    tenant_id    TEXT NOT NULL,
    rule_id      TEXT NOT NULL,
    priority     INTEGER NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    risk_class   TEXT NOT NULL,
    conditions   TEXT NOT NULL,
    actions      TEXT NOT NULL,
    notes        TEXT,
    updated_at   TEXT NOT NULL,
    updated_by   TEXT,
    PRIMARY KEY (tenant_id, rule_id)
);
CREATE INDEX IF NOT EXISTS idx_tenant_policy_rules_priority
    ON tenant_policy_rules(tenant_id, priority, rule_id);

-- 3) Server runtime overrides — env_overrides + last_restart bookkeeping.
CREATE TABLE IF NOT EXISTS tenant_servers_runtime (
    tenant_id           TEXT NOT NULL,
    server_id           TEXT NOT NULL,
    env_overrides       TEXT NOT NULL DEFAULT '{}',
    enabled             INTEGER NOT NULL DEFAULT 1,
    last_restart_at     TEXT,
    last_restart_reason TEXT,
    PRIMARY KEY (tenant_id, server_id)
);

-- 4) Entity activity — denormalised projection of audit_events, written by
--    the audit fanout. Original audit table remains canonical.
CREATE TABLE IF NOT EXISTS entity_activity (
    tenant_id     TEXT NOT NULL,
    entity_kind   TEXT NOT NULL,
    entity_id     TEXT NOT NULL,
    event_id      TEXT NOT NULL,
    occurred_at   TEXT NOT NULL,
    actor_user_id TEXT,
    summary       TEXT NOT NULL,
    diff_json     TEXT
);
CREATE INDEX IF NOT EXISTS idx_entity_activity_lookup
    ON entity_activity(tenant_id, entity_kind, entity_id, occurred_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (9);
