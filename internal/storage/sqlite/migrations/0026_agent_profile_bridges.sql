-- Phase 16 — MCP<->A2A bridge routes on the Agent Profile.
--
-- A bridge declares that a call in one protocol family transparently dispatches
-- to a backend in the other, governed by the same Agent Profile that owns the
-- route (CLAUDE.md §13 — the Profile is the single source of consumer routing +
-- entitlement). Two directions, two tables; tenant-scoped, FK-cascade.
--
--   mcp_to_a2a: an MCP tools/call for mcp_tool dispatches to A2A peer a2a_peer's
--               task a2a_task. One bridge per (profile, mcp_tool).
--   a2a_to_mcp: an inbound A2A task a2a_task dispatches to MCP tool mcp_tool.
--               One bridge per (profile, a2a_task).
CREATE TABLE IF NOT EXISTS agent_profile_mcp_to_a2a_bridges (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    mcp_tool    TEXT NOT NULL,              -- namespaced "server.tool"
    a2a_peer    TEXT NOT NULL,              -- registered peer name
    a2a_task    TEXT NOT NULL,              -- task id on the peer
    PRIMARY KEY (tenant_id, profile_id, mcp_tool),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_profile_a2a_to_mcp_bridges (
    tenant_id   TEXT NOT NULL,
    profile_id  TEXT NOT NULL,
    a2a_task    TEXT NOT NULL,              -- task name exposed inbound (namespaced "peer.task")
    mcp_tool    TEXT NOT NULL,              -- namespaced "server.tool"
    PRIMARY KEY (tenant_id, profile_id, a2a_task),
    FOREIGN KEY (tenant_id, profile_id) REFERENCES agent_profiles(tenant_id, id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (26);
