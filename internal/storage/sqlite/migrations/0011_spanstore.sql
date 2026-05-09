-- Phase 11: self-contained span store. The OTel exporter tees every
-- finished span into this table in addition to the configured external
-- collector. The session inspector reads spans from here so it works
-- without an external trace backend.
--
-- Tenant-scoped (every row carries tenant_id and the indexes lead with
-- it), forward-only.

CREATE TABLE IF NOT EXISTS spans (
    tenant_id    TEXT NOT NULL,
    session_id   TEXT,                   -- empty when the span isn't bound to a session
    trace_id     TEXT NOT NULL,
    span_id      TEXT NOT NULL,
    parent_id    TEXT,
    name         TEXT NOT NULL,
    kind         TEXT NOT NULL,          -- 'internal' | 'server' | 'client' | 'producer' | 'consumer'
    started_at   TEXT NOT NULL,          -- RFC3339 nano
    ended_at     TEXT NOT NULL,          -- RFC3339 nano
    status       TEXT NOT NULL,          -- 'unset' | 'ok' | 'error'
    status_msg   TEXT NOT NULL DEFAULT '',
    attrs_json   TEXT NOT NULL DEFAULT '{}',  -- canonical JSON of attribute set
    events_json  TEXT NOT NULL DEFAULT '[]',  -- canonical JSON of timed events (limited)
    PRIMARY KEY (tenant_id, trace_id, span_id)
);

CREATE INDEX IF NOT EXISTS idx_spans_session ON spans(tenant_id, session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_spans_trace   ON spans(tenant_id, trace_id, started_at);
CREATE INDEX IF NOT EXISTS idx_spans_started ON spans(tenant_id, started_at);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (11);
