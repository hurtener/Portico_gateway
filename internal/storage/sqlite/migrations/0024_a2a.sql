-- Phase 16 — A2A peers: external Agent-to-Agent endpoints a tenant registers.
-- Tenant-scoped (tenant_id NOT NULL, PK leads with tenant_id). agent_card_json
-- caches the peer's discovered agent card (refreshed by the ingestion unit).
CREATE TABLE IF NOT EXISTS a2a_peers (
    tenant_id        TEXT NOT NULL,
    id               TEXT NOT NULL,              -- ULID-style id
    name             TEXT NOT NULL,
    endpoint         TEXT NOT NULL,              -- peer base URL
    egress_auth_ref  TEXT,                       -- optional vault secret ref for egress auth
    agent_card_json  TEXT,                       -- cached discovered agent card (JSON), nullable
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, name)
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (24);
