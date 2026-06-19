-- Phase 16 — Agent Profile A2A allowlists.
--
-- Two more join tables on the Phase 14 agent_profiles schema, mirroring the
-- existing MCP server/tool join tables in 0020: which A2A peers (by name) and
-- which A2A tasks (namespaced "peer.task") a profile may reach. An empty
-- allowlist means "all" of that kind (same back-compat semantics as the MCP
-- server/tool allowlists). Tenant-scoped, FK-cascades to agent_profiles.
CREATE TABLE IF NOT EXISTS agent_profile_a2a_peers (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    peer_name   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, profile_id, peer_name),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_a2a_tasks (
    tenant_id     TEXT NOT NULL,
    profile_id    TEXT NOT NULL,
    namespaced_id TEXT NOT NULL,                     -- "research-agent.code-review"
    PRIMARY KEY (tenant_id, profile_id, namespaced_id),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (25);
