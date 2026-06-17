-- Phase 13: LLM chat sessions. Conversations the gateway brokered — distinct
-- from MCP sessions. One row per conversation + one row per message.
CREATE TABLE IF NOT EXISTS tenant_llm_sessions (
    tenant_id   TEXT NOT NULL,
    chat_id     TEXT NOT NULL,             -- ULID
    user_id     TEXT,
    alias       TEXT NOT NULL,             -- the model alias the chat used
    started_at  TEXT NOT NULL,
    ended_at    TEXT,
    summary     TEXT,
    PRIMARY KEY (tenant_id, chat_id)
);
CREATE INDEX IF NOT EXISTS idx_llm_sessions_recent ON tenant_llm_sessions(tenant_id, started_at DESC);

CREATE TABLE IF NOT EXISTS tenant_llm_messages (
    tenant_id    TEXT NOT NULL,
    chat_id      TEXT NOT NULL,
    seq          INTEGER NOT NULL,          -- monotonic per chat, starts at 1
    role         TEXT NOT NULL,             -- 'system' | 'user' | 'assistant' | 'tool'
    content_json TEXT NOT NULL,             -- canonical JSON; caller redacts before persistence
    tool_call_id TEXT,
    span_id      TEXT,                      -- link to the span the call produced
    created_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, chat_id, seq),
    FOREIGN KEY (tenant_id, chat_id) REFERENCES tenant_llm_sessions(tenant_id, chat_id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (18);
