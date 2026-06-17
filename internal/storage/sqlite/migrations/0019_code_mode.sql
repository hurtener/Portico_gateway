-- Phase 13.5 — Code Mode execution + continuation state.
--
-- code_mode_executions is an audit/abuse-review record of every executeToolCode
-- run. code_mode_continuations holds a SUSPENDED execution awaiting operator
-- approval: when an in-sandbox tool call returns approval_required, the runtime
-- serialises the snippet plus the results of every tool call that completed
-- BEFORE the awaited one, so the resume can replay deterministically (the
-- snapshot is immutable, the clock is frozen, and Starlark is deterministic
-- given identical bindings). Both tables are tenant-scoped: every query filters
-- WHERE tenant_id = ? so a continuation token from tenant A is invisible to
-- tenant B (acceptance #10, threat-model class C5).
CREATE TABLE IF NOT EXISTS code_mode_executions (
    tenant_id        TEXT NOT NULL,
    execution_id     TEXT NOT NULL,                 -- random 128-bit token
    session_id       TEXT NOT NULL,
    started_at       TEXT NOT NULL,
    finished_at      TEXT,
    status           TEXT NOT NULL,                 -- 'running'|'completed'|'failed'|'awaiting_approval'
    snippet_sha      TEXT NOT NULL,                 -- sha-256 of executed code (replay / abuse review)
    tool_calls       INTEGER NOT NULL DEFAULT 0,
    tokens_saved_est INTEGER NOT NULL DEFAULT 0,
    output_redacted  TEXT,                          -- redacted summary, never the full body
    span_id          TEXT NOT NULL,
    PRIMARY KEY (tenant_id, execution_id)
);
CREATE INDEX IF NOT EXISTS idx_code_mode_exec_by_session
    ON code_mode_executions(tenant_id, session_id, started_at DESC);

CREATE TABLE IF NOT EXISTS code_mode_continuations (
    tenant_id            TEXT NOT NULL,
    continuation_token   TEXT NOT NULL,             -- random 128-bit token, single-use
    execution_id         TEXT NOT NULL,
    session_id           TEXT NOT NULL,
    snapshot_id          TEXT NOT NULL,             -- pinned snapshot; resume fails closed on drift
    code                 TEXT NOT NULL,             -- the snippet, re-executed on resume
    cached_results       TEXT NOT NULL DEFAULT '[]',-- JSON array of result payloads for calls 0..awaiting-1
    awaiting_call_index  INTEGER NOT NULL,          -- ordinal of the tool call awaiting approval
    awaiting_approval_id TEXT NOT NULL,             -- the approval the runtime threads on resume
    print_buffer         TEXT NOT NULL DEFAULT '',  -- redacted print() snapshot (audit only; replay regenerates)
    clock_unix           INTEGER NOT NULL,          -- frozen time.now() seconds — replay determinism (class C4)
    created_at           TEXT NOT NULL,
    expires_at           TEXT NOT NULL,             -- created_at + TTL (default 24h)
    consumed_at          TEXT,                      -- set atomically on resume; single-use guard (double_resume)
    PRIMARY KEY (tenant_id, continuation_token),
    FOREIGN KEY (tenant_id, execution_id)
        REFERENCES code_mode_executions(tenant_id, execution_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_code_mode_cont_expiry
    ON code_mode_continuations(expires_at);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (19);
