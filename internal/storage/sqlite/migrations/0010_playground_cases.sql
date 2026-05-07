-- Phase 10: Interactive MCP Playground.
--
-- Two new tenant-scoped tables:
--   * playground_cases — saved test cases (tool_call|resource_read|prompt_get).
--   * playground_runs  — run history for cases AND ad-hoc executions.
--
-- Both tables follow the multi-tenant pattern: tenant_id is part of the
-- composite primary key and every read MUST filter on it.

CREATE TABLE IF NOT EXISTS playground_cases (
    tenant_id   TEXT NOT NULL,
    case_id     TEXT NOT NULL,                 -- ULID
    name        TEXT NOT NULL,
    description TEXT,
    kind        TEXT NOT NULL,                 -- 'tool_call' | 'resource_read' | 'prompt_get'
    target      TEXT NOT NULL,                 -- '<server>.<tool>' | uri | prompt name
    payload     TEXT NOT NULL,                 -- canonical JSON of the call shape
    snapshot_id TEXT,                          -- optional pin
    tags        TEXT NOT NULL DEFAULT '[]',    -- canonical JSON array
    created_at  TEXT NOT NULL,
    created_by  TEXT,
    PRIMARY KEY (tenant_id, case_id)
);
CREATE INDEX IF NOT EXISTS idx_playground_cases_tenant_created
    ON playground_cases(tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS playground_runs (
    tenant_id    TEXT NOT NULL,
    run_id       TEXT NOT NULL,                -- ULID
    case_id      TEXT,                         -- NULL for ad-hoc
    session_id   TEXT NOT NULL,
    snapshot_id  TEXT NOT NULL,
    started_at   TEXT NOT NULL,
    ended_at     TEXT,
    status       TEXT NOT NULL,                -- 'running'|'ok'|'error'|'denied'
    drift_detected INTEGER NOT NULL DEFAULT 0,
    summary      TEXT,                         -- short text for the case list
    PRIMARY KEY (tenant_id, run_id)
);
CREATE INDEX IF NOT EXISTS idx_playground_runs_lookup
    ON playground_runs(tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_playground_runs_case
    ON playground_runs(tenant_id, case_id, started_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (10);
