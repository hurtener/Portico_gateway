# Phase 15.5 — Semantic Cache, Virtual Keys, Hierarchical Budgets

> Self-contained implementation plan. Builds on Phases 0–13.5 (V1.5 + V1.6 LLM gateway + Code Mode) and Phase 14 (Agent Profiles). Adds three Bifrost-shaped governance primitives Portico needs to be SOTA against both agentgateway and Bifrost: **semantic caching** for LLM responses, **Virtual Keys** (`pk-portico-*` HMAC-bound credentials attached to Agent Profiles, with their own budgets/limits), and **hierarchical budgets** (VK → Team → Customer → Tenant). The phase is intentionally sized so it lands as one cohesive surface — the three pieces compose, and shipping them separately would force operators to redo the Console twice.
>
> **2026-05-12 update.** Phase 14 was rewritten on 2026-05-12 from "Envoy-shaped substrate" to "Agent Profiles." This Phase 15.5 plan referenced the old Phase 14 as a `Listener/Route/Backend` substrate; those references have been corrected. Virtual Keys now attach to an Agent Profile (Phase 14) — the Profile is the consumer primitive, the VK is the credential lifecycle. Budgets attach to VKs / Teams / Customers; the Profile is *not* a budget level.

## Goal

After Phase 15.5:

1. Operators can put a **semantic cache** in front of the LLM gateway, pluggable across Weaviate, Redis/Valkey, Qdrant, or "none" (the §4.4 seam). Cache hits return identical OpenAI-shaped responses with `cache_hit` telemetry, skipping the upstream provider call entirely. Both exact-hash and embedding-similarity modes are supported. Per-tenant namespacing is enforced at the cache-key level; cross-tenant collisions are impossible by construction.
2. Operators issue **Virtual Keys** (Portico-side `pk-portico-*` tokens) that sub-divide tenant identity into per-app / per-developer / per-environment slots. Each VK has its own scope set, its own allowed-providers/models filter, its own RPS / token / cost budget, and its own audit lineage. Revoking a VK doesn't touch any other VK or the parent tenant.
3. A **hierarchical budget model** (VK → Team → Customer → Tenant) means a finance ops user can say "the marketing department gets $5k/mo across all VKs it owns" and have that enforced uniformly. Per-level budgets accumulate; the lowest-level overage trips first; audit captures which level fired.

These three primitives are how a buyer comparing Portico to Bifrost (which has all of them) or to agentgateway (which has none in this shape, plus a thinner provider surface) sees Portico as the more complete consolidator. Crucially: the work *reuses* our existing Phase 5 vault, Phase 5 policy engine, Phase 5/9 quota machinery, and Phase 11 audit replay. Phase 15.5 doesn't bolt on a parallel governance plane; it deepens the one we already have.

## Why this phase exists (and why these three together)

Three independent reasons converge here:

1. **Cost.** Semantic caching alone tends to cut LLM bills 30–60% on customer-support-shaped and repeated-query workloads. Operators with non-trivial bills will demand this before they will demand A2A.
2. **Governance.** Multi-tenancy at the Portico level is the V1 invariant, but real organisations have *sub-tenant* structure — engineering vs. marketing, prod vs. dev, dev/staging/prod per microservice. Without Virtual Keys, every such sub-division ends up sharing one set of credentials and one budget. The first operator pain we hear post-V1.5 will be "my dev team is burning my prod budget."
3. **Hierarchical accountability.** Once VKs exist, a single budget cap on the tenant is the wrong shape — it forces operators to over-allocate to the loudest sub-team. Hierarchical budgets per VK / Team / Customer let finance ops manage the budget the same way they manage their org chart.

We ship the three together because:

- Semantic caching needs a key partition surface; VKs are the natural partition. Caching first then VKs would require a partition-key migration after VKs land.
- VKs without budgets are just a token-renaming exercise. The whole point of VK isolation is the budget that goes with it.
- Hierarchical budgets without VKs cannot be expressed (the "tenant" is the only level we have today).

Doing them in lockstep is one Console update, one migration set, one set of audit event types, one set of acceptance criteria.

## Prerequisites

- Phase 5 vault + policy engine + audit + redactor.
- Phase 11 span store + audit replay.
- Phase 13 LLM gateway + engine seam (`internal/llm/engine`).
- Phase 13.5 MCP Code Mode (the cache also applies to tool calls dispatched from inside Code Mode; the VK + budget model gates Code Mode executions too).
- Phase 14 Agent Profiles — VKs attach to a Profile (`profile_id` FK on `virtual_keys`); the Profile drives consumer-side gating, the VK is the credential. VK and Profile allowlists intersect (most-restrictive wins).
- A working production-shape Bifrost adapter (Phase 13 §"Architecture"). Bifrost's own `Account.GetKeysForProvider` is still authoritative for *provider* keys; **VKs are an additional, Portico-side layer** that sits in front and is invisible to Bifrost.

## Out of scope (explicit)

- **No exposed vector store as a tenant feature.** The vector store this phase introduces is internal to the semantic cache. Tenants do not get a `/v1/vector_store/*` API. (That is a future product surface; the §4.4 seam means we can light it up later without re-plumbing.)
- **No PII redaction inside the cache.** Cache entries respect the same redactor that audit uses; what is too sensitive to audit is too sensitive to cache. We do not add a new redactor.
- **No cross-tenant cache sharing.** Even when two tenants ask identical prompts of identical models, they get separate cache entries. (Within a tenant, cross-VK sharing is configurable via a per-Customer cache scope.)
- **No prompt-rewrite cache invalidation.** A model upgrade by the provider does not invalidate the cache (the model alias is part of the key). Operators flush cache by namespace; auto-flush on provider-model deprecation is a future enhancement.
- **No cache for tool calls outside the LLM gateway.** Phase 15.5 caches LLM completions / embeddings / Code-Mode tool dispatches that the LLM ultimately triggers. Direct MCP `tools/call` from a non-LLM client is not cached (the cost is in the call itself, and idempotency is the tool's concern).
- **No automated VK rotation.** VK rotation is a manual operator action; rotation reminders + bulk rotation tooling are a future enhancement.
- **No cross-cloud cache replication.** A multi-region deployment has one cache backend per region; replication is the operator's job (Weaviate/Redis-side).

## Deliverables

### A. Semantic cache

1. **`internal/llm/cache/ifaces/`** — `Cache` interface with `Lookup(ctx, key, opts) (*Entry, hit bool, err)`, `Store(ctx, key, entry) error`, `Invalidate(ctx, prefix) error`, plus a `Driver` interface (§4.4 seam pattern).
2. **Cache drivers**:
   - `internal/llm/cache/none/` — no-op driver (default).
   - `internal/llm/cache/redis/` — Redis/Valkey driver, exact-hash mode optimal. Pure Go (`github.com/redis/go-redis`). CGo-free.
   - `internal/llm/cache/weaviate/` — Weaviate driver, dual-layer (exact hash + semantic similarity). Embedding generator pluggable.
   - `internal/llm/cache/qdrant/` — Qdrant driver, semantic-similarity mode. Pure Go gRPC.
   - `internal/llm/cache/inmem/` — bounded-LRU in-memory driver (development + tests).
3. **Embedding generator interface** — for similarity mode, the cache needs embeddings of incoming prompts. The default generator reuses the operator's configured embedding model via the LLM gateway (so the same Portico-managed credentials apply). Operators can configure a "cache embedding model" alias separately if they want to use a cheap dedicated model.
4. **Cache-key composition** — `(tenant_id, scope: vk|team|customer|tenant, alias, normalized_input_hash, mode_specific_extras)`. Documented in `docs/concepts/semantic-cache.md`; operators can override scope per-route.
5. **Hit/miss telemetry** — cache hits add `llm.cache_hit: true`, `llm.cache_mode: exact|semantic`, `llm.cache_similarity: 0.0–1.0`, `llm.cache_lookup_ms`, `llm.cache_entry_age_s` to the span. Audit event `llm.cache_hit` per hit; `llm.cache_miss` aggregated per minute to avoid bloat.
6. **TTL + invalidation** — per-tenant default TTL (5 min Bifrost-style), configurable per route. `POST /api/llm/cache/invalidate` admits a `prefix` (e.g. all entries for alias=`gpt-4`, or all entries for VK=`pk-portico-xyz`); requires `admin` scope; emits `llm.cache_invalidated` audit event.
7. **Cache bypass headers** — clients can opt out per request via `Cache-Control: no-store` (treated as both no-write and no-read) or `Cache-Control: no-cache` (no-read, may still write). Bifrost's `x-bf-cache-*` headers are also recognised for compatibility. Operators can deny bypass via policy.

### B. Virtual Keys

8. **`internal/auth/virtual_keys/`** — VK lifecycle (create, list, revoke, rotate, scope, attach to Team/Customer), credential format (`pk-portico-<random40>`), HMAC-SHA256 binding so a leaked VK cannot impersonate a different tenant.
9. **VK auth strategy** — extends `internal/dataplane/middleware/jwt.go` (the single auth middleware Phase 14 introduced) to recognise `Authorization: Bearer pk-portico-…` as a VK and resolve it to its parent tenant + bound Agent Profile + scope set + budgets. JWTs continue to be the operator's primary auth path; VKs are a *programmatic* alternative for first-party app tokens. No `auth_egress` middleware (Phase 15 deferred); VKs are northbound credentials only.
10. **Per-VK scopes + allowlists** — every VK carries a scope set, a provider allowlist (e.g. "this VK may only call `anthropic`"), and a model allowlist (e.g. "only `claude-3-5-sonnet`"). A request that violates the allowlist fails with `403 vk_scope_violation` and audits.
11. **VK ↔ MCP** — VKs work for MCP requests too. A VK with `mcp:call` scope can dispatch tools via `/mcp/...`; per-server allowlists (`allowed_servers: ["github", "jira"]`) constrain which downstream MCP servers it sees in the catalog. Code Mode sessions inherit VK constraints.

### C. Hierarchical budgets

12. **`internal/budgets/`** — budget engine + repos. Resources: `customer`, `team`, `virtual_key`. A VK belongs to at most one Team OR one Customer OR neither (mutually exclusive, per Bifrost's pattern). Teams belong to one Customer.
13. **Budget definition** — each level supports independent budgets: `requests_per_period`, `tokens_per_period`, `cost_usd_per_period`. Periods: `1m`, `1h`, `1d`, `1w`, `1M` (calendar month), `1Y`. Reset alignment: rolling or calendar (UTC-aligned).
14. **Budget enforcement** — pre-call check is hierarchical (VK budget → Team budget → Customer budget → Tenant budget). The lowest violated level fires; the response says which (`429 budget_exceeded`, `details.level: vk|team|customer|tenant`, `details.metric: cost_usd|tokens|requests`). Post-call reconcile updates every applicable level atomically (one SQL transaction).
15. **Budget admin** — REST + Console CRUD for Customers, Teams, VKs, Budgets. List pages get `+ Add` CTAs per the §4.5.1 operator UX gates.
16. **Budget warnings** — at 80% of any budget, an `llm.budget_warning` audit event fires (once per warning window — debounced). At 95%, an `llm.budget_critical` audit event fires plus an optional webhook (configurable per Customer). At 100%, the budget enforces (denies the call).

### D. Cross-cutting

17. **Policy extensions** — new matchers (`vk.id`, `vk.scope`, `vk.team`, `vk.customer`, `cache.would_hit`, `budget.headroom_pct`) and new actions (`deny_on_cache_miss`, `force_cache_bypass`, `clamp_to_customer_budget`). Same engine, additive rules.
18. **REST API + CLI** — full CRUD per resource (Customers, Teams, VKs, Budgets, Cache config).
19. **Console** — `/governance` parent route containing `/customers`, `/teams`, `/virtual-keys`, `/budgets`, `/cache`. Each list page with `+ Add`, edit forms covering the full spec, Playwright per §4.5.1.
20. **Smoke** — `scripts/smoke/phase-15.5.sh` covers semantic-cache hit/miss + invalidation, VK happy path + scope violation + revocation, hierarchical-budget tier firing + reconciliation atomicity, MCP path with a VK.
21. **Conformance suite extension** — `cmd/portico conformance --suite openai` adds tests for `Cache-Control` headers, VK bearer auth, and `budget_exceeded` error shape.

## Acceptance criteria

### Semantic cache

1. **Exact-hash hit.** Two identical chat requests (same alias, same normalized prompt) on the same tenant return identical responses; the second one is served from cache with `cache_hit: true, mode: exact, lookup_ms < 5`. No upstream provider call occurs (mock provider records exactly one call).
2. **Semantic hit.** With Weaviate driver + threshold 0.85, two paraphrased prompts produce one upstream call and one cache hit with `mode: semantic, similarity ≥ 0.85`. Documented in test fixture so reviewers can replay.
3. **TTL expiry.** A cache entry with TTL=10s expires; the 11th-second request is a miss and produces a fresh upstream call.
4. **Per-tenant isolation.** Tenant A's cache hit never satisfies tenant B's request, even for identical prompts. Cross-tenant integration test asserts.
5. **Per-VK scope (when configured).** With `cache.scope: vk`, two VKs in the same tenant cache separately; with `cache.scope: tenant`, they share. Both modes have integration tests.
6. **Cache bypass headers honoured.** `Cache-Control: no-store` produces no cache write and no cache read; the response does not include `cache_hit`. `Cache-Control: no-cache` reads from upstream but writes the result for future lookups.
7. **Invalidation.** `POST /api/llm/cache/invalidate?prefix=alias=gpt-4` removes every matching entry; subsequent lookups miss; an audit event records the operator and the prefix.
8. **Cache + tool-use.** A streaming chat completion with `tool_calls` is **not** cached (the conversation state varies per tool result). A non-tool-use chat with identical inputs **is** cached. Documented; test asserts.
9. **Cache + Code Mode.** A Code Mode session's `executeToolCode` snippet that calls a tool with identical args returns the cached tool result if the policy enables tool-call caching at that route. (Off by default — tool calls are typically not safe to cache without explicit operator opt-in.)
10. **Driver swap is config-only.** Switching `cache.driver: redis` → `cache.driver: weaviate` requires no code change; the smoke runs against both legs in CI matrix.

### Virtual Keys

11. **Create + use.** `POST /api/governance/virtual-keys` returns a `pk-portico-...` value (shown once); subsequent `Authorization: Bearer pk-portico-...` on `/v1/chat/completions` succeeds.
12. **Scope enforcement.** A VK with `scopes: ["llm:invoke"]` succeeds at `/v1/chat/completions`; the same VK fails with `403 vk_scope_violation` at `/v1/embeddings` (which requires `llm:embed`).
13. **Provider/model allowlist.** A VK restricted to `providers: ["anthropic"], models: ["claude-3-5-sonnet"]` fails with `403 vk_scope_violation` when invoking `gpt-4`. Audit event records the violation.
14. **Revocation is immediate.** A `DELETE /api/governance/virtual-keys/{id}` makes the next call fail with `401 vk_revoked`. No retroactive in-flight cancellation; in-flight requests complete (consistent with our other auth paths).
15. **Rotation preserves identity.** `POST /api/governance/virtual-keys/{id}/rotate` returns a new secret; the old secret stops authenticating; budgets and audit history attached to the VK ID are preserved (no orphaning).
16. **MCP path works.** A VK with `mcp:call` scope dispatches `/mcp/tools/call` against an allowed downstream server; a disallowed server produces `403 vk_scope_violation`.
17. **Cross-tenant impossibility.** A VK from tenant A cannot resolve under tenant B's namespace; an attempt to use one fails with `401 vk_unknown` (deliberately ambiguous to avoid VK enumeration).

### Hierarchical budgets

18. **VK-level firing.** A VK with `cost_usd_per_period: 1.00, period: 1h` is denied with `429 budget_exceeded, level: vk, metric: cost_usd` when the in-flight call would push it over.
19. **Team-level firing.** A VK whose Team has `cost_usd_per_period: 5.00, period: 1d` is denied with `level: team` when its Team aggregate trips, even if its own VK budget has headroom.
20. **Customer-level firing.** Same logic, `level: customer`.
21. **Lowest level wins.** When VK and Team would both fire, the response says `level: vk` (we report the most specific level so operators know what to raise).
22. **Reconciliation atomicity.** A successful call updates VK + Team + Customer ledgers atomically in one transaction. A test injects a fault between the call and the reconcile, asserts no level is updated without the others.
23. **Warning audit events.** At 80%/95%/100%, the right events fire exactly once per warning window. Test asserts no duplicate warnings on subsequent calls within the window.
24. **Calendar vs. rolling reset.** Both alignments work; tests assert the window boundary.
25. **Budget read API.** `GET /api/governance/virtual-keys/{id}/budget` returns `{period, used, limit, resets_at, headroom_pct, parents: [{level: team, used, limit, ...}, {level: customer, used, limit, ...}]}`. Used by the Console dashboard.

### Cross-cutting

26. **Console UX gates.** Per §4.5.1: every list page (Customers, Teams, VKs, Budgets, Cache config) has a `+ Add` CTA; every form covers the full spec; Playwright covers create + edit + delete; new Form components auto-generate label ids.
27. **Cross-tenant isolation.** Integration test covers every new resource. VKs, Teams, Customers, Cache entries, Budget ledgers — none cross tenant boundaries.
28. **Audit redaction.** Audit events containing VK secrets / cache keys / budget values pass through the redactor; VK secrets are never stored in plaintext beyond initial issuance (we store `salt + hmac`).
29. **Smoke gate.** `scripts/smoke/phase-15.5.sh` shows OK ≥ 20, FAIL = 0; prior phases' smokes still pass.
30. **Coverage.** `internal/llm/cache/...` ≥ 80%; `internal/auth/virtual_keys/...` ≥ 90% (auth-critical); `internal/budgets/...` ≥ 85% (correctness-critical for cost reporting).
31. **Performance.** p95 cache lookup ≤ 10 ms (Redis), ≤ 25 ms (Weaviate semantic). Per-request VK resolution overhead ≤ 1 ms (in-memory cache, refreshed on rotation/revocation events). Per-request budget check ≤ 2 ms.

## Architecture

### Package layout

```
internal/llm/cache/
├── ifaces/
│   └── cache.go               # Cache + Driver interfaces
├── cache.go                   # Open(name, cfg) factory + registry
├── key.go                     # cache-key composition + normalization
├── none/
│   └── driver.go              # no-op default
├── inmem/
│   └── driver.go              # bounded LRU
├── redis/
│   └── driver.go              # exact-hash mode optimal
├── weaviate/
│   └── driver.go              # dual-layer (exact + semantic)
├── qdrant/
│   └── driver.go
├── embeddings/
│   ├── ifaces/embedding.go
│   └── llm_gateway/           # default generator routes through LLM gateway
└── cache_test.go              # cross-driver matrix tests

internal/auth/virtual_keys/
├── vk.go                      # VK type, create/rotate/revoke
├── format.go                  # pk-portico-* format + HMAC binding
├── resolver.go                # bearer string -> (tenant, scope, budgets)
├── cache.go                   # in-memory LRU with TTL + revoke broadcast
└── vk_test.go

internal/budgets/
├── budget.go                  # Budget + Ledger types
├── store.go                   # repos (Customer, Team, Budget, Ledger)
├── enforcer.go                # hierarchical pre-check
├── reconcile.go               # atomic post-call update
├── window.go                  # rolling vs. calendar alignment
└── enforcer_test.go

internal/dataplane/middleware/
├── jwt.go                     # extended: also recognises pk-portico-* VK bearer
├── budget.go                  # the hierarchical pre-check middleware
└── budget_test.go

internal/storage/sqlite/migrations/
├── 0017_governance.sql        # customers, teams, virtual_keys, vk_provider_allowlist, vk_model_allowlist
├── 0018_budgets.sql           # budgets, budget_ledgers
└── 0019_semantic_cache.sql    # cache_entries (when using SQLite-backed cache for tests)

internal/server/api/
├── governance_customers.go
├── governance_teams.go
├── governance_virtual_keys.go
├── governance_budgets.go
└── llm_cache.go               # invalidate + status

cmd/portico/
├── cmd_governance.go          # `portico governance customers|teams|vk|budgets ...`
└── cache_wiring.go            # blank imports of cache drivers (the §4.4 seam)

web/console/src/routes/governance/
├── +layout.svelte             # nav for sub-pages
├── customers/
├── teams/
├── virtual-keys/
├── budgets/
└── cache/
    ├── +page.svelte           # cache config + invalidate UI
    └── cache.spec.ts
```

### SQL DDL

#### Migration 0017 — governance entities

```sql
CREATE TABLE IF NOT EXISTS governance_customers (
    tenant_id   TEXT NOT NULL,
    id          TEXT NOT NULL,             -- ULID
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS governance_teams (
    tenant_id    TEXT NOT NULL,
    id           TEXT NOT NULL,
    customer_id  TEXT,                     -- nullable; team may stand alone under tenant
    name         TEXT NOT NULL,
    description  TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES governance_customers(tenant_id, id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS governance_virtual_keys (
    tenant_id    TEXT NOT NULL,
    id           TEXT NOT NULL,                -- ULID
    name         TEXT NOT NULL,
    salt         BLOB NOT NULL,
    hmac         BLOB NOT NULL,                -- HMAC-SHA256(salt, secret); secret never stored
    parent_kind  TEXT NOT NULL CHECK (parent_kind IN ('none','team','customer')),
    parent_id    TEXT,                         -- references team or customer when parent_kind != 'none'
    scopes       TEXT NOT NULL DEFAULT '[]',   -- JSON array
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL,
    rotated_at   TEXT,
    revoked_at   TEXT,
    PRIMARY KEY (tenant_id, id)
);

CREATE TABLE IF NOT EXISTS vk_provider_allowlist (
    tenant_id TEXT NOT NULL,
    vk_id     TEXT NOT NULL,
    provider_driver TEXT NOT NULL,            -- e.g. 'anthropic', 'custom_openai'
    PRIMARY KEY (tenant_id, vk_id, provider_driver),
    FOREIGN KEY (tenant_id, vk_id) REFERENCES governance_virtual_keys(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS vk_model_allowlist (
    tenant_id TEXT NOT NULL,
    vk_id     TEXT NOT NULL,
    alias     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, vk_id, alias),
    FOREIGN KEY (tenant_id, vk_id) REFERENCES governance_virtual_keys(tenant_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS vk_mcp_server_allowlist (
    tenant_id TEXT NOT NULL,
    vk_id     TEXT NOT NULL,
    server_id TEXT NOT NULL,
    PRIMARY KEY (tenant_id, vk_id, server_id),
    FOREIGN KEY (tenant_id, vk_id) REFERENCES governance_virtual_keys(tenant_id, id) ON DELETE CASCADE
);
```

#### Migration 0018 — budgets + ledgers

```sql
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
    tenant_id  TEXT NOT NULL,
    budget_id  TEXT NOT NULL,
    window_key TEXT NOT NULL,                  -- normalised window identifier (e.g. '2026-05-12T13')
    used       REAL NOT NULL DEFAULT 0,
    resets_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, budget_id, window_key),
    FOREIGN KEY (tenant_id, budget_id) REFERENCES governance_budgets(tenant_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_budget_ledger_lookup ON governance_budget_ledger(tenant_id, budget_id, resets_at DESC);
```

#### Migration 0019 — cache entries (SQLite-backed dev driver only)

```sql
CREATE TABLE IF NOT EXISTS llm_cache_entries (
    tenant_id  TEXT NOT NULL,
    cache_key  TEXT NOT NULL,
    mode       TEXT NOT NULL,                  -- 'exact'|'semantic'
    alias      TEXT NOT NULL,
    payload    BLOB NOT NULL,                  -- serialised response, redactor-applied
    embedding  BLOB,                           -- semantic mode only
    similarity REAL,                           -- denormalised for analytics
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, cache_key)
);
CREATE INDEX IF NOT EXISTS idx_cache_entries_expiry ON llm_cache_entries(tenant_id, expires_at);
```

### Cache interface

```go
// internal/llm/cache/ifaces/cache.go
package ifaces

type Cache interface {
    Name() string
    Lookup(ctx context.Context, key Key, opts LookupOpts) (*Entry, bool, error)
    Store(ctx context.Context, key Key, entry Entry) error
    Invalidate(ctx context.Context, prefix Prefix) (int, error)
    Stats(ctx context.Context, tenantID string) (Stats, error)
    Close(ctx context.Context) error
}

type Key struct {
    TenantID         string
    Scope            Scope            // vk | team | customer | tenant
    ScopeID          string
    Alias            string           // model alias
    NormalizedInput  []byte           // canonicalised; hashed by the driver
    SimilarityVector []float32        // optional; semantic mode
    Mode             Mode             // exact | semantic | both
    ExtraSalt        []byte           // operator-supplied per route (e.g. system-prompt fingerprint)
}

type Entry struct {
    Payload    []byte                  // serialised ChatResponse / EmbeddingResponse
    Mode       Mode
    Similarity float32
    CreatedAt  time.Time
    ExpiresAt  time.Time
    Tokens     int
    CostUSD    float64
}

type Driver interface {
    Name() string
    New(cfg map[string]any, deps Deps) (Cache, error)
}
```

### VK lifecycle

VK secrets are issued once at creation. We store `salt + HMAC-SHA256(salt, secret)` and never see the secret again. Resolution flow on `Authorization: Bearer pk-portico-XXXX`:

1. Parse the bearer; reject if the prefix or charset is wrong (no DB call on malformed inputs).
2. Look up by `id_hint` (first 8 chars of the suffix encode the tenant + VK identifier prefix to make resolution O(1)) — we accept some discoverability of which tenant a VK belongs to in exchange for not iterating all VKs.
3. Verify HMAC.
4. Reject if `revoked_at IS NOT NULL` or `enabled = 0`.
5. Hydrate scopes, allowlists, and budget hierarchy into the request context.

The resolver caches the hydrated state in-memory (`internal/auth/virtual_keys/cache.go`) with a 60 s TTL. Revocations broadcast a `vk.revoked` event over the internal pub/sub so revocation is effective within ~1 s across instances (Phase 19 Redis-coordinator integrates).

### Budget enforcement

Pre-call: walk from most-specific (VK) to least-specific (Tenant). For each enabled budget, compute the candidate next-used value as `current_used + cost_estimate`. If any level exceeds, deny with the most specific level. The enforcement is one `SELECT` against the ledger per active budget (≤ 4 normally, bounded).

Post-call: in one transaction, `INSERT OR UPDATE` the ledger for each applicable level using the actual consumed metric values. A test injects a SQL fault mid-transaction and asserts no partial updates.

Warnings fire on transitions: when `used / limit` crosses 80% (and a warning hasn't fired in the current window) emit `llm.budget_warning` once; same logic for 95%. We track "last warning level" per ledger row to make the transition idempotent.

### REST API

Governance CRUD (per-tenant, admin scope for cross-tenant):

```
GET    /api/governance/customers
POST   /api/governance/customers
GET    /api/governance/customers/{id}
PUT    /api/governance/customers/{id}
DELETE /api/governance/customers/{id}

GET    /api/governance/teams
POST   /api/governance/teams
…

GET    /api/governance/virtual-keys
POST   /api/governance/virtual-keys              # returns the secret once
GET    /api/governance/virtual-keys/{id}         # never returns the secret
POST   /api/governance/virtual-keys/{id}/rotate  # returns the new secret once
DELETE /api/governance/virtual-keys/{id}         # revoke
GET    /api/governance/virtual-keys/{id}/budget  # hierarchical headroom

GET    /api/governance/budgets
POST   /api/governance/budgets
PUT    /api/governance/budgets/{id}
DELETE /api/governance/budgets/{id}

GET    /api/llm/cache/config
PUT    /api/llm/cache/config                     # which driver, TTL, threshold, scope
POST   /api/llm/cache/invalidate
GET    /api/llm/cache/stats                      # per-tenant hit rate, entry count
```

### CLI

```bash
portico governance customers list|create|update|delete
portico governance teams list|create|update|delete
portico governance vk list|create|rotate|revoke
portico governance budgets list|create|update|delete

portico llm cache config get|put
portico llm cache invalidate --prefix alias=gpt-4
portico llm cache stats
```

### Console screens

`/governance/customers`, `/teams`, `/virtual-keys`, `/budgets`, `/cache` — full CRUD per §4.5.1. The Virtual Keys list page shows VK name, scopes, attached parent (Team / Customer / "none"), enabled status, last used; the detail page shows the budget hierarchy with live headroom bars (VK / Team / Customer / Tenant stacked); rotation and revocation are obvious actions; the create flow surfaces the secret exactly once with a "Copy" button and a stern "you won't see this again" notice.

`/governance/budgets` lets operators define a budget for any (scope_kind, scope_id, metric, period) tuple. List view shows live used vs. limit. Detail view shows the period boundary and a 7-day sparkline.

`/governance/cache` is a single-page admin: pick driver (Weaviate / Redis / Qdrant / inmem / none), set TTL + similarity threshold + scope, hit "Test" to verify connectivity, "Invalidate prefix..." action. Live stats card with hit rate over the last 24 h.

## Implementation walkthrough

### Step 1 — Migrations + repos

Land 0017–0019. Implement repos for customers, teams, virtual_keys, allowlists, budgets, ledger. Round-trip tests for every JSON column.

### Step 2 — VK auth strategy

Implement `internal/auth/virtual_keys/`. Wire the resolver into `internal/dataplane/middleware/jwt.go` so `Authorization: Bearer pk-portico-...` routes through the resolver instead of the JWT validator. Add the in-memory cache + revocation broadcast.

Tests: every acceptance criterion in the VK section.

### Step 3 — Budget engine

Implement `internal/budgets/`. The enforcer middleware is new; it runs *after* tenant/JWT/VK resolution and *before* policy. Add the warning dispatcher.

Tests: every acceptance criterion in the Budgets section. Atomic-reconcile test uses a `sqlite3` fault injector.

### Step 4 — Cache interface + drivers

Define the interface, write the no-op + inmem + Redis drivers. Then Weaviate + Qdrant. Each driver registers from `init()`; the factory dispatches by name. Configuration loads driver-specific blocks under `cache:` in `portico.yaml`.

Tests: cross-driver matrix runs the same canonical scenarios against every driver.

### Step 5 — Cache integration in the LLM gateway

Hook the cache into `internal/llm/gateway/handler.go`. The flow becomes:

1. Resolve tenant, VK, scope, budget — middleware (Steps 2–3).
2. Resolve provider + alias + model — handler.
3. Compute cache key from (tenant, scope, scope_id, alias, normalized request).
4. Lookup. On hit, return immediately; emit `llm.cache_hit` audit, span, ledger update for the response shape's documented "0 cost" (or operator-configured `cache_hit_cost_usd`).
5. On miss, dispatch via the engine; on success, store; emit `llm.cache_miss` (aggregated).

The Streaming path is more nuanced: a streaming response can only be cached if we materialise it; we default to no-cache for `stream: true` requests, and offer a "materialise-and-cache" option per route (off by default).

### Step 6 — Console screens

`/governance/*` CRUD. Per §4.5.1: every list page has `+ Add`, every form is complete, Playwright covers create/edit/delete on every entity, label ids auto-generated.

### Step 7 — Smoke + tests

`scripts/smoke/phase-15.5.sh`:

- Create a Customer, Team, VK.
- Make a call with the VK against `/v1/chat/completions` → 200.
- Replay the same call → cache hit; assert `cache_hit: true` in response metadata.
- Replay with `Cache-Control: no-store` → cache miss + no write.
- Invalidate by alias prefix → next call misses.
- Set a $0.01 VK budget; spend it; next call → 429 with `level: vk`.
- Use the VK on `/mcp/...` → 200 against allowed server; → 403 against disallowed.
- Rotate the VK; old secret → 401; new secret → 200.
- Set a Team budget more restrictive than VK; trip it; → 429 with `level: team`.
- Invalidate the cache for `vk=<id>` prefix → only that VK's entries removed.

OK ≥ 20 by phase close, FAIL = 0.

## Test plan

### Unit (next to source)

- `internal/llm/cache/cache_test.go` — driver matrix: exact-hash hit, semantic hit, TTL expiry, invalidation, per-tenant isolation, scope variation.
- `internal/llm/cache/redis/driver_test.go` — Redis-specific behaviours (mocked via `miniredis`).
- `internal/llm/cache/weaviate/driver_test.go` — Weaviate-specific (mocked HTTP).
- `internal/llm/cache/qdrant/driver_test.go` — Qdrant-specific (mocked gRPC).
- `internal/auth/virtual_keys/vk_test.go`
  - `TestVK_CreateReturnsSecretOnce`
  - `TestVK_HMACVerifies`
  - `TestVK_RotationPreservesIdentity`
  - `TestVK_RevocationImmediate`
  - `TestVK_ResolverCacheRespectsRevocation`
- `internal/budgets/enforcer_test.go`
  - `TestEnforcer_VKLevelFires`
  - `TestEnforcer_TeamLevelFires`
  - `TestEnforcer_CustomerLevelFires`
  - `TestEnforcer_LowestLevelWins`
  - `TestEnforcer_ReconcileAtomic`
  - `TestEnforcer_WarningsDebounced`
  - `TestEnforcer_CalendarVsRolling_BoundaryCorrect`

### Integration (`test/integration/governance/`)

- `TestE2E_VK_HappyPath`
- `TestE2E_VK_ScopeViolation`
- `TestE2E_VK_AllowlistViolation`
- `TestE2E_VK_OnMCPPath`
- `TestE2E_Budget_VK_Tier_Fires`
- `TestE2E_Budget_Team_Tier_Fires`
- `TestE2E_Budget_Customer_Tier_Fires`
- `TestE2E_Budget_AtomicReconcile_NoPartialUpdate`
- `TestE2E_Cache_ExactHit`
- `TestE2E_Cache_SemanticHit`
- `TestE2E_Cache_TenantIsolation`
- `TestE2E_Cache_VKScope_Vs_TenantScope`
- `TestE2E_Cache_StreamingRequestNotCachedByDefault`
- `TestE2E_Cache_InvalidationByPrefix`
- `TestE2E_Governance_AllResourcesTenantIsolated`

### Frontend tests

- Playwright: Customer/Team/VK create + edit + delete; VK rotation flow (secret shown once); Budget create + live headroom rendering; Cache config swap + invalidate; cross-resource link navigation.

### Smoke

`scripts/smoke/phase-15.5.sh` — listed above. OK ≥ 20.

### Coverage gates

- `internal/llm/cache/...` ≥ 80%; `redis/` ≥ 85%; `weaviate/` ≥ 80%.
- `internal/auth/virtual_keys/...` ≥ 90%.
- `internal/budgets/...` ≥ 85%.

## Common pitfalls

- **VK secret storage in plaintext.** Never. HMAC only. A test injects a known secret and asserts no plaintext appears anywhere in the DB.
- **Cache returning a response from another tenant.** The cache key partitions by tenant; the lookup function asserts the tenant in the returned entry matches the request tenant before delivering. Defence in depth.
- **Semantic similarity false positives.** Default threshold 0.85; configurable. Too low produces wrong answers; too high makes the cache useless. Document the trade-off; expose the per-call similarity in the response so operators can tune.
- **Budget race conditions.** Two concurrent calls can both pass the pre-check and both push the ledger over. Reconciliation must be atomic *and* the pre-check must run inside the same logical lock (per-VK mutex; coarse-grained but fine for typical traffic shapes). Tests stress this.
- **Cache TTL drift across drivers.** Redis uses its own TTL; Weaviate uses ours; ensure consistent semantics. The driver interface mandates expiration at write; tests assert.
- **VK bearer collision with provider keys.** Provider keys (`sk-...`) and VKs (`pk-portico-...`) share the bearer slot. The auth strategy distinguishes by prefix. Tests assert that a `sk-...` bearer never resolves as a VK and vice versa.
- **MCP allowlist bypass via Code Mode.** A VK with `allowed_servers: ["github"]` must not be able to call `jira` tools through a Code Mode sandbox. The dispatcher's allowlist check runs *after* the sandbox's tool resolution, on the same tool. Integration test asserts.
- **Cache + tool-use semantics.** Tool-use responses are conversation-state-dependent. We do NOT cache by default. Operators who opt in for idempotent tool flows accept the responsibility.
- **Cache invalidation thunderingherd.** A `POST /invalidate?prefix=alias=gpt-4` on a large cache can be slow. The driver implements prefix invalidation asynchronously when supported (Weaviate), returns 202 with an invalidation_id, and exposes status. Operators see progress in the Console.
- **Hierarchical budget cycles.** A misconfigured VK with `parent_kind: team` pointing at a team in another tenant is rejected at write time. A team pointing at a deleted customer cascades correctly per the FK.
- **Webhook on budget critical.** Optional outbound webhooks must be queued + retried; never block the request path. The webhook delivery service lives in `internal/governance/webhooks/` and is bounded-channel-backed like the audit pipeline.
- **Embedding generation cost.** Every cache lookup in semantic mode generates an embedding. If the embedding model is expensive, semantic cache *increases* cost. Document; default the cache embedding model to a cheap one; surface "cache embedding cost" in the cache stats card.
- **Console secret-display footgun.** The VK secret is shown once. The Modal must not be dismissable until the operator clicks "I've saved it." Playwright asserts.

## Out of scope (recap, plus a few specifics)

- No automated VK rotation (cron-style) — manual only.
- No webhook authoring UI — operators set webhook URLs as plain text strings; full webhook DSL is a future enhancement.
- No PII redaction in cache (uses the audit redactor).
- No SaaS-mode quota marketplace.
- No cross-region cache replication (operator-owned at the backend layer).
- No cache for streaming responses by default.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-15.5.sh` shows OK ≥ 20, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. `portico conformance --suite openai` extended with cache-bypass-header + VK-bearer + `budget_exceeded` cases — passes.
5. Docs site gains: `/docs/concepts/semantic-cache`, `/docs/concepts/virtual-keys`, `/docs/concepts/hierarchical-budgets`, `/docs/how-to/cache-driver-swap`, `/docs/how-to/vk-lifecycle`, `/docs/how-to/budget-strategy`.
6. RFC-001 updated with a Governance section consolidating VKs, Teams, Customers, Budgets, Semantic Cache.
7. `AGENTS.md` §13 forbidden practices updated:
   - "Storing Virtual Key secrets in plaintext, in logs, or in any non-HMAC form."
   - "Issuing a Virtual Key without a `salt` and `HMAC` pair persisted before the secret is returned to the operator."
   - "Caching LLM responses across tenant boundaries, regardless of cache driver."
   - "Pre-check approving a request that the post-call reconcile cannot fund (race-free pre-check + reconcile is mandatory)."
8. `docs/plans/README.md` index updated.

## Hand-off to Phase 16+

- **Phase 16 (A2A)** — VKs apply uniformly to A2A inbound auth. Cache extends to A2A task responses when operators opt in.
- **Phase 17 (tool poisoning)** — budgets become a soft signal: anomalous spend (e.g. a VK whose burn rate triples in an hour) feeds into the security scoreboard.
- **Phase 18 (dynamic config API)** — governance resources (Customers, Teams, VKs, Budgets, Cache config) are first-class on the watch channel.
- **Phase 19 (production scale-out)** — Postgres-backed governance + Redis-backed VK resolver cache + Redis pub/sub for revocation broadcast across instances + Weaviate/Qdrant as the prod cache choice. Tenant-scoped governance never crosses federation boundaries.
