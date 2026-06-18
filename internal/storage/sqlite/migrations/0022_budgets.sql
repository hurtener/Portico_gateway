-- Phase 15.5 — Hierarchical budgets + ledgers.
CREATE TABLE IF NOT EXISTS governance_budgets (
    tenant_id   TEXT NOT NULL,
    id          TEXT NOT NULL,                -- ULID
    scope_kind  TEXT NOT NULL CHECK (scope_kind IN ('vk','team','customer','tenant')),
    scope_id    TEXT NOT NULL,                -- VK id / team id / customer id / tenant_id
    metric      TEXT NOT NULL CHECK (metric IN ('requests','tokens','cost_usd')),
    period      TEXT NOT NULL CHECK (period IN ('1m','1h','1d','1w','1M','1Y')),
    alignment   TEXT NOT NULL CHECK (alignment IN ('rolling','calendar')),
    limit_val   REAL NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, scope_kind, scope_id, metric, period)
);

CREATE TABLE IF NOT EXISTS governance_budget_ledger (
    tenant_id          TEXT NOT NULL,
    budget_id          TEXT NOT NULL,
    window_key         TEXT NOT NULL,         -- normalised window id (e.g. '2026-05-12T13')
    used               REAL NOT NULL DEFAULT 0,
    resets_at          TEXT NOT NULL,
    last_warning_level INTEGER NOT NULL DEFAULT 0, -- 0/80/95/100 for debounced warnings
    PRIMARY KEY (tenant_id, budget_id, window_key),
    FOREIGN KEY (tenant_id, budget_id) REFERENCES governance_budgets(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_budget_ledger_lookup ON governance_budget_ledger(tenant_id, budget_id, resets_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (22);
