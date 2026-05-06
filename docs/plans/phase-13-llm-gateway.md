# Phase 13 — LLM Gateway (V1.5)

> Self-contained implementation plan. Builds on Phase 0–12. **Post-V1.** Adds an LLM gateway alongside the MCP gateway without changing V1 invariants.

## Goal

Make Portico an LLM gateway as well as an MCP gateway, using `github.com/kreuzberg-dev/liter-llm` as the engine. Single binary, same multi-tenant guarantees, same observability stack, same audit + policy + credential surface. Northbound it speaks OpenAI's HTTP API (chat completions, completions, embeddings, models list); southbound it routes to whichever providers the operator registers (OpenAI, Anthropic, Azure OpenAI, Bedrock, local Ollama, etc., via liter-llm's adapters).

The phase's strategic value: agents already get tool surfaces from Portico via MCP. Phase 13 adds the model surface — so a customer or an internal developer can point a single base URL at Portico and get both governed model access AND governed tool access from one place. Tool use bridges back into the MCP gateway: when a model returns a tool call in OpenAI format, Portico translates to MCP, dispatches via the existing northbound flow, and feeds the result back into the model loop.

## Why this phase exists (and why now)

User feedback: "for LLM Gateway I meant liter-llm which is a newly created client that has a Go client directly. There were several security attacks to LiteLLM in the last month and we even lost keys, so it's off the table. […] Telemetry visualization first-class for the replay, then we extend with the LLM gateway."

The brief is clear:

- **Use `kreuzberg-dev/liter-llm`**, not BerriAI/litellm. Trustworthy author, Go-native, no Python in the binary.
- **Telemetry visualization first** (Phase 11), then LLM gateway. Phase 11 produced a replay surface that handles spans + audit + drift uniformly; Phase 13 extends those data structures to LLM calls (token usage, model latency, retry chains, tool-call loops).
- **No security shortcuts.** Per-tenant key vaulting via the existing Phase 5 vault. No passthrough by default. Every prompt + response goes through the redactor + audit store. Every model call is policy-evaluated.

Phase 13 sits in V1.5 territory — it's not required for the V1 ship — but the user wants it on the roadmap with the same plan-quality bar as the V1 phases.

## Prerequisites

Phases 0–12 complete (V1 shipped). Specifically:

- Vault (Phase 5) — for provider API keys.
- Policy engine (Phase 5/9) — extended here to evaluate LLM calls.
- Audit + redactor (Phase 5/11) — events for prompts, responses, token usage.
- Span store + bundle export (Phase 11) — LLM calls produce spans of the same shape; bundles capture them.
- MCP dispatcher (Phase 1) + skills runtime (Phase 4) — for tool-use bridging.
- Console CRUD pattern (Phase 9) — for model registry CRUD.
- Console primitives (Phase 7) — tables, forms, code blocks, schema-driven forms (Phase 10).
- Docs + conformance pattern (Phase 12) — extended with LLM gateway docs + an OpenAI conformance suite.
- `github.com/kreuzberg-dev/liter-llm` published as a stable Go module.

## Deliverables

1. **liter-llm integration** — `internal/llm/` houses the adapter to liter-llm. The library is integrated as a Go dependency, no sidecar process. CGo-free; static binary unchanged.
2. **OpenAI-compatible northbound API** — `/v1/chat/completions`, `/v1/completions`, `/v1/embeddings`, `/v1/models`, `/v1/moderations`, streaming and non-streaming. Authentication via the same JWT validator the MCP gateway uses (with new scopes: `llm:invoke`, `llm:embed`, `llm:moderate`).
3. **Provider registry** — CRUD over LLM provider configurations (driver = openai | anthropic | azure_openai | bedrock | ollama | …). Per-tenant. Each provider points at a vault entry for credentials. Endpoint, region, organisation id, default model are configurable.
4. **Model registry** — operator names model aliases that map onto provider+model pairs. Tenant A's `gpt-4` can resolve to Azure OpenAI's `gpt-4-deployment-1`; Tenant B's `gpt-4` can resolve to OpenAI's `gpt-4o`. Aliases are first-class — clients refer to aliases, not provider models.
5. **Per-tenant key vaulting** — every provider API key lives in the Phase 5 vault, keyed by `(tenant, provider, key_purpose)`. Keys never leave the gateway. liter-llm gets a one-shot accessor that reads from vault on every request (no in-memory caching).
6. **Tool-use bridging** — when a model emits an OpenAI tool call (`tool_calls` with name + arguments), Portico translates to MCP `tools/call` against the matching tool in the live catalog (subject to the policy engine + approval flow), feeds the tool result back into the model loop, and continues the conversation. The bridge respects MCP's elicitation flow (model-requested approval is queued the same way an external client's would be).
7. **Quotas + rate limits** — per-tenant token quotas (input/output/total per minute, per day) and request-per-minute caps. Defaults sane; configurable. Exceeded quotas return `429 quota_exceeded` with a typed error body.
8. **Observability** — every LLM call produces a span with attributes: `llm.provider`, `llm.model`, `llm.prompt_tokens`, `llm.completion_tokens`, `llm.cost_usd` (when computable), `llm.tool_calls`. Spans land in the Phase 11 span store. Audit events: `llm.invoked`, `llm.streamed`, `llm.tool_bridged`, `llm.quota_exceeded`, `llm.failed`.
9. **Policy extension** — policy rules gain LLM-specific matchers (provider, model alias, prompt regex, max tokens) and actions (deny, require_approval, redirect_to_alternative_model).
10. **Console screens** — `/llm/providers`, `/llm/models`, `/llm/quotas`, `/llm/sessions` (chat replay reusing Phase 11 inspector), an LLM playground at `/llm/playground` (similar shape to Phase 10's MCP playground but with chat and completion modes).
11. **Cost telemetry** — model unit costs configurable per provider; cost_usd computed per call and aggregated in `/llm/cost` dashboards (per tenant, per model, per day). Operators can set per-tenant budgets that emit warnings + denials at thresholds.
12. **Conformance suite extension** — `cmd/portico conformance --suite openai` runs OpenAI-API conformance against the live binary so a tenant can validate their integration.

## Acceptance criteria

1. liter-llm pulls in cleanly. `CGO_ENABLED=0 go build` succeeds. Binary size delta vs. Phase 12 ≤ +20 MB.
2. OpenAI clients (Python `openai`, JS `openai`, `curl`) work end-to-end against `http://localhost:8080/v1/...` with a bearer token issued by Portico's JWT machinery.
3. Streaming responses (Server-Sent Events for chat completions) work. Backpressure-safe. Heartbeat comments every 15 s on long generations.
4. Model alias resolution: a request to `gpt-4` for tenant A reaches the configured provider+model; same alias for tenant B reaches a different provider+model. No cross-tenant leakage.
5. Vault key never visible in logs, audit, or error messages. A test deliberately injects a known token shape and asserts it never appears in any persistence layer except the encrypted vault.
6. Tool-use bridging: a model that returns a tool call (OpenAI format) for a tool that exists in the MCP catalog dispatches via the existing dispatcher, the result feeds back, the model continues, and the full transcript records every bridged call as a child span.
7. Quotas: a tenant exceeding its per-minute token cap receives `429 quota_exceeded`; the failed request is audit-logged; subsequent allowed requests work after the window.
8. Cost telemetry: 1000 known-shape calls produce a cost figure within 1% of the manually-computed total.
9. Policy: a deny rule on `model: gpt-4` blocks the request before the provider call; an audit event records the decision; provider key never used.
10. Smoke: `scripts/smoke/phase-13.sh` covers providers CRUD, models CRUD, chat completion (non-streaming + streaming), embeddings, tool-use bridging, quota exceedance. SKIP for unimplemented; OK ≥ 12 by phase close.
11. Coverage: ≥ 75% across new packages; ≥ 80% on `internal/llm/gateway`, `internal/llm/bridge`, `internal/llm/quota`.
12. Cross-tenant isolation: integration test asserts every isolation invariant from Phase 0 + every LLM-specific one (provider keys, model aliases, quotas, conversations).

## Architecture

```
internal/llm/
├── gateway/
│   ├── handler.go                # /v1/* endpoints
│   ├── streaming.go              # SSE framing
│   ├── auth.go                   # bearer extraction + JWT validation
│   └── handler_test.go
├── adapters/
│   └── liter.go                  # wraps github.com/kreuzberg-dev/liter-llm
├── providers/
│   ├── provider.go               # Provider interface (driver-agnostic)
│   ├── store.go                  # SQLite-backed registry (per tenant)
│   └── store_test.go
├── models/
│   ├── alias.go                  # alias resolver
│   ├── store.go
│   └── store_test.go
├── bridge/
│   ├── tool_bridge.go            # OpenAI tool_call ↔ MCP tools/call
│   ├── elicitation.go            # model-initiated approval routing
│   └── tool_bridge_test.go
├── quota/
│   ├── enforcer.go               # token + RPM enforcement
│   ├── window.go                 # rolling-window counter
│   └── enforcer_test.go
├── cost/
│   ├── calculator.go             # per-provider unit costs
│   ├── ledger.go                 # per-tenant cost rollups
│   └── calculator_test.go
└── policy_extensions.go          # LLM matcher/action support for the policy engine

internal/storage/sqlite/migrations/
├── 0013_llm_providers_models.sql
├── 0014_llm_quotas_costs.sql
└── 0015_llm_sessions.sql

internal/server/api/
├── llm_providers.go
├── llm_models.go
├── llm_quotas.go
├── llm_costs.go
└── llm_sessions.go               # chat session replay (reuses Phase 11 bundle shape)

cmd/portico/
└── cmd_conformance.go            # extended with --suite openai

web/console/src/routes/llm/
├── providers/
├── models/
├── quotas/
├── costs/
├── playground/
└── sessions/
```

The northbound MCP gateway (Phase 1+) and the LLM gateway share the listener, the JWT validator, the audit machinery, and the span store. They diverge at the URL prefix (`/api/`, `/mcp/`, `/v1/`) and the dispatcher.

## SQL DDL

### Migration 0013 — providers + models

```sql
CREATE TABLE IF NOT EXISTS tenant_llm_providers (
    tenant_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    driver        TEXT NOT NULL,            -- 'openai' | 'anthropic' | 'azure_openai' | 'bedrock' | 'ollama' | …
    config_json   TEXT NOT NULL,            -- driver-specific (endpoint, region, org_id, …)
    credential_ref TEXT,                    -- vault key
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS tenant_llm_models (
    tenant_id     TEXT NOT NULL,
    alias         TEXT NOT NULL,            -- e.g. 'gpt-4', 'fast-summary'
    provider_name TEXT NOT NULL,            -- references tenant_llm_providers
    provider_model TEXT NOT NULL,           -- e.g. 'gpt-4o', 'claude-3-5-sonnet-20241022'
    default_params_json TEXT NOT NULL DEFAULT '{}', -- temperature, top_p, max_tokens
    capabilities  TEXT NOT NULL DEFAULT '[]', -- JSON array: 'chat'|'completion'|'embedding'|'moderation'|'tool_use'
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, alias),
    FOREIGN KEY (tenant_id, provider_name) REFERENCES tenant_llm_providers(tenant_id, name)
);
```

### Migration 0014 — quotas + costs

```sql
CREATE TABLE IF NOT EXISTS tenant_llm_quotas (
    tenant_id           TEXT NOT NULL,
    requests_per_minute INTEGER NOT NULL DEFAULT 600,
    tokens_per_minute   INTEGER NOT NULL DEFAULT 200000,
    tokens_per_day      INTEGER NOT NULL DEFAULT 4000000,
    cost_usd_per_day    REAL    NOT NULL DEFAULT 100.00,
    PRIMARY KEY (tenant_id)
);

CREATE TABLE IF NOT EXISTS llm_unit_costs (
    provider_driver TEXT NOT NULL,         -- 'openai', 'anthropic', …
    provider_model  TEXT NOT NULL,
    input_per_1k    REAL NOT NULL,
    output_per_1k   REAL NOT NULL,
    PRIMARY KEY (provider_driver, provider_model)
);

-- Cost ledger per tenant per day (rolled up, not per-call)
CREATE TABLE IF NOT EXISTS tenant_llm_cost_daily (
    tenant_id   TEXT NOT NULL,
    day         TEXT NOT NULL,             -- YYYY-MM-DD UTC
    alias       TEXT NOT NULL,
    requests    INTEGER NOT NULL DEFAULT 0,
    input_tok   INTEGER NOT NULL DEFAULT 0,
    output_tok  INTEGER NOT NULL DEFAULT 0,
    cost_usd    REAL    NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day, alias)
);
CREATE INDEX IF NOT EXISTS idx_llm_cost_daily ON tenant_llm_cost_daily(tenant_id, day DESC, alias);
```

### Migration 0015 — chat sessions

```sql
-- LLM "chat sessions" are conversations — distinct from MCP sessions.
CREATE TABLE IF NOT EXISTS tenant_llm_sessions (
    tenant_id   TEXT NOT NULL,
    chat_id     TEXT NOT NULL,             -- ULID
    user_id     TEXT,
    alias       TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    ended_at    TEXT,
    summary     TEXT,
    PRIMARY KEY (tenant_id, chat_id)
);

CREATE TABLE IF NOT EXISTS tenant_llm_messages (
    tenant_id   TEXT NOT NULL,
    chat_id     TEXT NOT NULL,
    seq         INTEGER NOT NULL,           -- monotonic per chat
    role        TEXT NOT NULL,              -- 'system' | 'user' | 'assistant' | 'tool'
    content_json TEXT NOT NULL,             -- canonical JSON; redacted before persistence
    tool_call_id TEXT,
    span_id     TEXT,                       -- link to the span the call produced
    created_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, chat_id, seq),
    FOREIGN KEY (tenant_id, chat_id) REFERENCES tenant_llm_sessions(tenant_id, chat_id) ON DELETE CASCADE
);
```

## Public types

```go
// internal/llm/providers/provider.go

type Provider struct {
    Name          string
    Driver        string
    Config        map[string]any
    CredentialRef string
    Enabled       bool
}

type Repo interface {
    List(ctx context.Context, tenantID string) ([]Provider, error)
    Get(ctx context.Context, tenantID, name string) (*Provider, error)
    Put(ctx context.Context, tenantID string, p Provider) error
    Delete(ctx context.Context, tenantID, name string) error
}
```

```go
// internal/llm/models/alias.go

type Model struct {
    Alias          string
    ProviderName   string
    ProviderModel  string
    DefaultParams  map[string]any
    Capabilities   []string
    Enabled        bool
}

type Resolver interface {
    Resolve(ctx context.Context, tenantID, alias string) (*Model, error)
    List(ctx context.Context, tenantID string) ([]Model, error)
}
```

```go
// internal/llm/gateway/handler.go

type ChatRequest struct {
    Model            string                 `json:"model"`           // alias
    Messages         []ChatMessage          `json:"messages"`
    Stream           bool                   `json:"stream"`
    MaxTokens        int                    `json:"max_tokens,omitempty"`
    Temperature      *float64               `json:"temperature,omitempty"`
    Tools            []ToolDef              `json:"tools,omitempty"`   // OpenAI tool format
    ToolChoice       *ToolChoice            `json:"tool_choice,omitempty"`
    Metadata         map[string]string      `json:"metadata,omitempty"`
}

type ChatMessage struct {
    Role       string       `json:"role"`        // 'system' | 'user' | 'assistant' | 'tool'
    Content    string       `json:"content,omitempty"`
    ToolCalls  []ToolCall   `json:"tool_calls,omitempty"`
    ToolCallID string       `json:"tool_call_id,omitempty"`
    Name       string       `json:"name,omitempty"`
}

type ToolCall struct {
    ID       string         `json:"id"`
    Type     string         `json:"type"`        // "function"
    Function FunctionCall   `json:"function"`
}

type FunctionCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON-encoded string per OpenAI convention
}
```

```go
// internal/llm/bridge/tool_bridge.go

// ToOpenAITools materialises every MCP tool the calling tenant's session sees
// as an OpenAI tool definition. The model picks one (or none); the bridge
// translates the call back to MCP, dispatches, and feeds the result.
func ToOpenAITools(ctx context.Context, tenantID, sessionID string) ([]gateway.ToolDef, error)

// HandleToolCall translates an OpenAI tool call into an MCP tools/call,
// dispatches via the existing dispatcher, applies policy + approval +
// credential injection, and returns the tool result as an OpenAI tool
// message ready to feed back into the model loop.
func HandleToolCall(ctx context.Context, tenantID, sessionID string, call gateway.ToolCall) (gateway.ChatMessage, error)
```

```go
// internal/llm/quota/enforcer.go

type Enforcer interface {
    Check(ctx context.Context, tenantID string, est Estimate) error // returns ErrQuotaExceeded
    Record(ctx context.Context, tenantID string, actual Actual) error
}

type Estimate struct {
    InputTokens int
}

type Actual struct {
    InputTokens  int
    OutputTokens int
    CostUSD      float64
    ProviderName string
    Alias        string
}

var ErrQuotaExceeded = errors.New("quota_exceeded")
```

```go
// internal/llm/cost/calculator.go

type UnitCost struct {
    Driver         string
    ProviderModel  string
    InputPer1K     float64
    OutputPer1K    float64
}

func Compute(input, output int, uc UnitCost) float64
```

## REST API

OpenAI-compatible (selected — full surface mirrors the OpenAI HTTP API):

```
POST   /v1/chat/completions                # streamed via stream=true
POST   /v1/completions
POST   /v1/embeddings
POST   /v1/moderations
GET    /v1/models                          # tenant's enabled aliases
GET    /v1/models/{alias}                  # detail
```

Portico-native admin surface:

```
GET    /api/llm/providers
POST   /api/llm/providers
GET    /api/llm/providers/{name}
PUT    /api/llm/providers/{name}
DELETE /api/llm/providers/{name}

GET    /api/llm/models
POST   /api/llm/models
GET    /api/llm/models/{alias}
PUT    /api/llm/models/{alias}
DELETE /api/llm/models/{alias}

GET    /api/llm/quotas
PUT    /api/llm/quotas

GET    /api/llm/costs?day=YYYY-MM-DD          # rollup
GET    /api/llm/costs/by-day?from=…&to=…       # time series
GET    /api/llm/costs/by-model?day=…           # per-model breakdown

GET    /api/llm/sessions                       # list chat sessions
GET    /api/llm/sessions/{chat_id}             # full transcript (redacted)
POST   /api/llm/sessions/{chat_id}/replay      # rerun against current providers
```

Errors uniform with `docs/plans/README.md`. New typed slugs: `quota_exceeded`, `provider_unavailable`, `model_unknown`, `unsupported_capability`, `policy_denied_llm`.

## CLI

```bash
portico llm providers list|get|put|delete
portico llm models list|get|put|delete
portico llm quotas get|put
portico llm costs --day YYYY-MM-DD

# Conformance against OpenAI surface
portico conformance --suite openai --target http://localhost:8080 --token "$JWT"
```

## Console screens

### `/llm/providers`

`Table`: name, driver, endpoint, status. Add via Modal: driver picker (`Select`) → driver-specific fields. Credential picker dropdown (Phase 9 vault). "Test connectivity" button issues a no-op call (e.g. list-models) and reports success/failure.

### `/llm/models`

`Table`: alias, provider, provider_model, capabilities, enabled. Add via Modal: alias text, provider picker, provider model dropdown (lazy-loaded from the provider's `/models` API), capability checkboxes, default params editor.

### `/llm/quotas`

Form: per-tenant quotas. Live indicator showing current minute's usage as a percentage bar. "Reset minute counter" admin action behind approval.

### `/llm/costs`

Two views:

- **By day** — line chart (svg, no charting library) of cost_usd over the last 30 days.
- **By model** — `Table` aggregated for a date or range; export CSV.

### `/llm/playground`

Layout similar to Phase 10's MCP playground:

- Left: alias picker + capability filter.
- Centre: chat composer (multi-turn). System message, user messages, optional tool selection (drawn from the live MCP catalog so tool-use bridging is testable here).
- Right: span tree + audit + cost + tokens — all live.

A "Save as case" button is reused from Phase 10. Replay reruns the conversation against the current providers (cost may differ; the replay UI surfaces the delta).

### `/llm/sessions`

`Table` of chat sessions with role + token + cost summaries. Click → transcript view (read-only) + "Replay" CTA (Phase 11 inspector pattern).

## Implementation walkthrough

### Step 1 — Migrations + repos

Land migrations 0013–0015. Implement provider + model + quota + cost repos. Round-trip tests cover canonical-JSON round trip on `config_json`, `default_params_json`, etc.

### Step 2 — liter-llm adapter

`internal/llm/adapters/liter.go` is a thin wrapper: takes a `Provider` + `Model`, mints a liter-llm client per request (or pools if liter-llm exposes a pooling primitive), forwards the call, surfaces typed errors. The adapter is the only file that imports `kreuzberg-dev/liter-llm` directly; everywhere else uses the `Provider` + `Model` abstractions.

### Step 3 — Northbound handler

`internal/llm/gateway/handler.go` parses OpenAI-shaped requests, validates the JWT, resolves the alias, evaluates policy, checks the quota, dispatches via the adapter, records cost, emits audit + span. Streaming path uses the existing SSE framing helpers from Phase 1's northbound transport.

### Step 4 — Tool-use bridge

When a chat request includes `tools: []`, the handler resolves the requested tool names against the live MCP catalog (per the calling tenant + session shape) and inlines their JSON Schemas into the OpenAI tool definitions. When the model returns `tool_calls`, the bridge dispatches via `internal/mcp/dispatcher` (Phase 1) — same code path an external MCP client uses. Approval, redaction, audit all apply.

The bridge handles the loop: model → tool call → MCP dispatch → tool result → model continuation. Loop count is capped (default 10; configurable per tenant) to avoid runaway cycles. Each iteration produces a span.

### Step 5 — Quota enforcer

Rolling-window counters per tenant. Two layers:

- Pre-call estimate (from prompt token count) checks against `tokens_per_minute` and `requests_per_minute`. Fails fast with `429`.
- Post-call actuals reconcile against the rolling window; if reconciliation pushes over the limit, the *next* call fails (we don't retroactively fail in-flight calls).

Daily quotas (`tokens_per_day`, `cost_usd_per_day`) checked against the rolled-up cost ledger. Approaching threshold (80%) emits `llm.quota_warning` audit event.

### Step 6 — Cost calculator + ledger

Unit costs seeded from a curated table (`internal/llm/cost/seeds.go`) covering the major providers. Operators can override per-deployment via `POST /api/llm/costs/units`. Per-call costs roll up into `tenant_llm_cost_daily`.

### Step 7 — Policy extension

`internal/llm/policy_extensions.go` adds matchers (`provider`, `model_alias`, `prompt_regex`, `max_tokens_gt`) and actions (`deny`, `require_approval`, `redirect_to_alias: <alt>`). The redirect action is interesting: it lets an operator say "if a tenant requests `gpt-4`, route to `gpt-4o-mini` instead" — useful for cost control without breaking client integrations.

### Step 8 — Console screens

CRUD pages follow the Phase 9 pattern. Playground reuses Phase 10 components (chat is a thin layer over `SchemaForm` for tool definitions + a multi-line input for messages). Cost dashboards use SVG; no charting library.

### Step 9 — OpenAI conformance suite

`internal/conformance/tests/openai/*.go` adds tests:

- `chat/completions` round-trip + streaming.
- `tool_calls` round-trip.
- `embeddings`.
- `models/list` returns aliases.
- Error surfaces (model_unknown, quota_exceeded, policy_denied_llm).
- Streaming heartbeat.

### Step 10 — Smoke + tests

`scripts/smoke/phase-13.sh` covers providers + models CRUD, chat completion, embeddings, tool-bridging happy path, quota exceedance, conformance summary.

## Test plan

### Unit

- `internal/llm/providers/store_test.go` — CRUD + tenant isolation + invalid driver rejected.
- `internal/llm/models/store_test.go` — alias resolution + capability filter + missing provider rejected.
- `internal/llm/gateway/handler_test.go`
  - `TestChatCompletion_NonStreaming_HappyPath`.
  - `TestChatCompletion_Streaming_HappyPath`.
  - `TestChatCompletion_AliasResolution_PerTenant`.
  - `TestChatCompletion_PolicyDeny_ShortCircuits`.
  - `TestChatCompletion_QuotaExceeded`.
  - `TestEmbeddings_HappyPath`.
- `internal/llm/bridge/tool_bridge_test.go`
  - `TestBridge_TranslatesToolCall`.
  - `TestBridge_ApprovalRequired_DefersResume`.
  - `TestBridge_LoopCap_Honored`.
- `internal/llm/quota/enforcer_test.go`
  - `TestEnforcer_PerMinute_Window`.
  - `TestEnforcer_PerDay_Aggregate`.
  - `TestEnforcer_ConcurrentCallsIncrementAtomically`.
- `internal/llm/cost/calculator_test.go`
  - `TestCompute_SeededProviders_KnownAnswer`.
  - `TestCompute_UnknownModel_ReturnsZero`.

### Integration (`test/integration/llm/`)

- `TestE2E_OpenAIClient_Compat` — drive `/v1/chat/completions` with the official `openai` Go client; full round-trip.
- `TestE2E_StreamingResponse_AllChunksOrdered`.
- `TestE2E_ToolBridge_DispatchesAndContinues`.
- `TestE2E_VaultKey_NeverLogged` — known token shape; assert it never appears in spans/audit/logs/error responses.
- `TestE2E_PerTenantAlias_Isolated`.
- `TestE2E_QuotaExceeded_Returns429`.
- `TestE2E_CostLedger_Accurate` — 100 calls at known unit cost; ledger == sum.
- `TestE2E_PolicyRedirect_AliasSwitched`.
- `TestE2E_TenantIsolation_Comprehensive` — every LLM table.

### Frontend tests

- Playwright: provider create + test connectivity, model alias create, playground chat happy path, cost dashboard renders, policy redirect respected.

### Smoke

`scripts/smoke/phase-13.sh`:
- POST `/api/llm/providers` (mock provider with httptest backing) → 201.
- POST `/api/llm/models` → 201.
- POST `/v1/chat/completions` → 200 + valid OpenAI shape.
- POST `/v1/chat/completions` with `stream: true` → SSE chunks ordered + final `[DONE]`.
- POST `/v1/embeddings` → 200.
- GET `/api/llm/costs?day=…` → 200.
- POST `/v1/chat/completions` past quota → 429 + `quota_exceeded` slug.

OK ≥ 12 by phase close, FAIL = 0.

### Coverage gates

- `internal/llm/gateway`: ≥ 80%.
- `internal/llm/bridge`: ≥ 80%.
- `internal/llm/quota`: ≥ 80%.
- `internal/llm/cost`: ≥ 75%.
- `internal/llm/providers`, `internal/llm/models`: ≥ 75%.

## Common pitfalls

- **liter-llm dependency creep.** Vet the dependency graph it introduces — pin versions, check for CGo, run a fresh `go.sum` audit. Any new transitive that breaks `CGO_ENABLED=0` is a blocker.
- **Provider key in error messages.** Some upstream errors echo the request verbatim. Wrap every provider error with a redactor that scrubs known-shape tokens (`sk-[A-Za-z0-9]{32,}`, `xai-…`, `anthropic-…`, etc.) before bubbling.
- **Streaming back-pressure.** A slow client receiving SSE can stall the response goroutine. Use a per-connection bounded channel + drop with `audit.dropped` event on overflow; never block the upstream.
- **Tool-use loops without cap.** A model that keeps requesting tools without progress chews tokens + cost. Loop cap mandatory; emit `llm.tool_loop_exceeded` audit when hit.
- **Cross-provider response shape drift.** liter-llm normalises this for you, but a stray provider-specific field can leak into the response. Run a JSON Schema check on every outbound payload against the OpenAI response shape.
- **Quota races.** Two simultaneous requests can both pass the pre-check and both push the counter over. Counter increment must use atomic SQL `UPDATE … SET v = v + ? WHERE …` and the pre-check must read the row in the same transaction.
- **Cost rounding drift.** Floating-point cost computation accumulates error. Round to 6 decimal places at write; tests assert sums are exact for known inputs.
- **Replay against deprecated models.** A saved chat session for a model the provider has deprecated will fail. The replay UI surfaces "model unavailable" with a "remap to alternative" CTA.
- **Audit payload bloat.** Full prompt + response per call is huge. Audit stores a redacted *summary* (first 256 chars + token counts + tool call summaries); the full content lands in `tenant_llm_messages` and is gated behind a separate scope.
- **Policy on prompt content.** Regex on prompts is fragile and bypassable. Document this clearly: prompt-content rules are advisory; deny by model+tool+capability is the sturdy enforcement layer.
- **OpenAI client version drift.** OpenAI evolves the API; some clients send fields liter-llm may not yet support. Reject unknown fields with a typed error rather than silently dropping them.
- **Multi-region keys.** A tenant with US and EU keys for the same provider needs two provider entries (one per region). Document the pattern; don't try to encode multi-region into a single provider row.

## Out of scope

- **Fine-tuning APIs.** `/v1/fine_tuning/*` — post-V1.5.
- **Files API + assistants API.** Out of scope; doesn't fit the gateway model.
- **Streaming embeddings.** The OpenAI surface doesn't stream embeddings; we don't either.
- **Function-calling fall-back for non-tool-use models.** If a model doesn't support tool use, the request includes tools but the model can't use them. We surface `unsupported_capability`; we do not synthesise tool use via prompt engineering.
- **Hosted SaaS variant.** Per RFC §15.
- **Per-user (sub-tenant) cost attribution.** Cost is per tenant; per-user is post-V1.5.
- **Custom embedding indices.** Vector store integration is a different product surface.
- **Image / audio modalities.** Phase 13 ships text only; multimodal is a follow-up.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-13.sh` shows OK ≥ 12, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. `portico conformance --suite openai` against `portico dev` passes 100%.
5. Bundle size + image size deltas documented in the PR description.
6. Docs site gains `/docs/concepts/llm-gateway` + how-tos for adding a provider, registering a model, setting quotas, debugging a chat session.
7. CHANGELOG updated with V1.5 entry.

## Hand-off to Phase 14+

V1.5 is shipped. Subsequent phases are additive nice-to-haves. Likely candidates inherit:

- The dual-gateway shape (MCP + LLM) — additional gateways (e.g. embedding-only, text-to-image) follow the same pattern: register a provider type, add a northbound handler, wire to the policy + audit + cost machinery.
- The chat session schema — extending it with images, audio, or arbitrary modalities is incremental.
- The conformance + smoke + docs pipeline — every new gateway grows them in lockstep.

The stable invariants: multi-tenant from V1, headless approval, credentials behind the gateway. Phase 13 inherits all three; any future phase must too.
