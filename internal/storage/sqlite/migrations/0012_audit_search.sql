-- Phase 11: full-text search over audit events.
--
-- Adds a denormalised `summary` column (one-line redacted preview that
-- emitter populates from the payload before insert) so FTS doesn't have
-- to pay the cost of scanning the raw JSON payload at query time.
--
-- Then attaches an FTS5 virtual table mirroring (type, summary,
-- payload_json) and triggers that keep it in sync. unicode61 tokenizer
-- so non-ASCII error text is searchable.

ALTER TABLE audit_events ADD COLUMN summary TEXT NOT NULL DEFAULT '';

CREATE VIRTUAL TABLE IF NOT EXISTS audit_events_fts USING fts5(
    type,
    summary,
    payload_json,
    content='audit_events',
    content_rowid='rowid',
    tokenize='unicode61'
);

-- Backfill: pull every existing row into the FTS index. Cheap on a fresh
-- DB; bounded by audit retention on existing deployments.
INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
SELECT rowid, type, summary, payload_json FROM audit_events;

CREATE TRIGGER IF NOT EXISTS audit_events_ai AFTER INSERT ON audit_events BEGIN
    INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
    VALUES (new.rowid, new.type, COALESCE(new.summary, ''), COALESCE(new.payload_json, ''));
END;

CREATE TRIGGER IF NOT EXISTS audit_events_ad AFTER DELETE ON audit_events BEGIN
    INSERT INTO audit_events_fts(audit_events_fts, rowid, type, summary, payload_json)
    VALUES ('delete', old.rowid, old.type, COALESCE(old.summary, ''), COALESCE(old.payload_json, ''));
END;

CREATE TRIGGER IF NOT EXISTS audit_events_au AFTER UPDATE ON audit_events BEGIN
    INSERT INTO audit_events_fts(audit_events_fts, rowid, type, summary, payload_json)
    VALUES ('delete', old.rowid, old.type, COALESCE(old.summary, ''), COALESCE(old.payload_json, ''));
    INSERT INTO audit_events_fts(rowid, type, summary, payload_json)
    VALUES (new.rowid, new.type, COALESCE(new.summary, ''), COALESCE(new.payload_json, ''));
END;

INSERT OR IGNORE INTO schema_migrations(version) VALUES (12);
