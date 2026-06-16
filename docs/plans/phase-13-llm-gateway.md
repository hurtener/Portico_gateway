# Phase 13 — LLM Gateway (V1.5)

> Self-contained implementation plan. Builds on Phase 0–12. **Post-V1.** Adds an LLM gateway alongside the MCP gateway without changing V1 invariants.
>
> **2026-05-12 revision.** The earlier draft of this phase wired the gateway to `github.com/kreuzberg-dev/liter-llm`. That dependency turned out to require CGo and ship a dependency footprint that fights the Portico single-binary invariant. We replace it with **[Bifrost](https://github.com/maximhq/bifrost) (Apache 2.0, pure-Go, CGo-free)** as the inference engine. Bifrost natively understands 23+ providers (a superset of the OpenAI-compatible list `agentgateway` ships) and has first-class hooks for operator-defined custom providers, which we use to close the remaining gap.

## Goal

Make Portico an LLM gateway as well as an MCP gateway, using the Bifrost Go SDK (`github.com/maximhq/bifrost/core`) as the inference engine. Single binary, same multi-tenant guarantees, same observability stack, same audit + policy + credential surface. Northbound it speaks OpenAI's HTTP API (chat completions, completions, embeddings, models list); southbound it routes to whichever providers the operator registers (OpenAI, Anthropic, Azure OpenAI, Bedrock, Vertex AI, Gemini, Groq, Mistral, Cohere, OpenRouter, Ollama, vLLM, xAI, plus operator-defined custom OpenAI-compatible upstreams).

The phase's strategic value: agents already get tool surfaces from Portico via MCP. Phase 13 adds the model surface — so a customer or an internal developer can point a single base URL at Portico and get both governed model access AND governed tool access from one place. Tool use bridges back into the MCP gateway: when a model returns a tool call in OpenAI format, Portico translates to MCP, dispatches via the existing northbound flow, and feeds the result back into the model loop.

## Why this phase exists (and why Bifrost, not liter-llm)

The original brief was "use kreuzberg-dev/liter-llm, not BerriAI/litellm." Two things changed:

1. The CGo footprint of `liter-llm` (transitive `mattn/sqlite3`, native crypto bindings on macOS) breaks `CGO_ENABLED=0`. We tried it; the binary stopped being statically linkable. That's a hard invariant from §7 / §13 of `AGENTS.md`.
2. The Bifrost SDK (also Go-native, also OpenAI-compatible northbound) reached general availability with: 23 native providers, virtual-key + hierarchical-budget governance primitives, semantic caching, an MCP integration with Code Mode, sub-100 μs overhead at 5k RPS, Apache 2.0 license. It's a better fit on every axis we picked liter-llm for.

The revised brief:

- **Use the Bifrost Go SDK** (`github.com/maximhq/bifrost/core`). Pure Go. Apache 2.0. We pin a release tag in `go.mod` and audit it like any other dependency.
- **Custom providers cover the agentgateway gap**. Bifrost natively serves OpenAI, Anthropic, Azure, Bedrock, Cerebras, Cohere, ElevenLabs, Fireworks, Gemini, Groq, HuggingFace, Mistral, Nebius, Ollama, OpenRouter, Parasail, Perplexity, Replicate, Runway, SGL, Vertex AI, vLLM, xAI. `agentgateway`'s extra openai-compatible names (DeepSeek, plus regional/enterprise OpenAI-compatible endpoints, plus `httpbun`-style mocks) are wired through the Bifrost "custom provider" surface we expose as a first-class operator concept.
- **No security shortcuts.** Per-tenant key vaulting via the existing Phase 5 vault. No passthrough by default. Every prompt + response goes through the redactor + audit store. Every model call is policy-evaluated.
- **Bifrost is a library, not a sidecar.** We never spawn `bifrost-http`. Portico stays one binary, one listener, one identity. Bifrost's internal HTTP-mode features (its own Console UI, its own VK store, its own admin REST) are not exposed externally; we re-use Bifrost's *engine* (providers, routing, semantic cache, plugins) and surface *our own* control plane (REST API + Console) on top, so multi-tenancy and the rest of the V1 envelope stay authoritative.

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
- `github.com/maximhq/bifrost/core` pinned to a stable release tag and license-audited (Apache 2.0).

## Deliverables

1. **Bifrost integration** — `internal/llm/engine/bifrost/` houses the adapter wrapping `github.com/maximhq/bifrost/core`. The library is integrated as a Go dependency, no sidecar process. CGo-free; static binary unchanged. The adapter is the **only** package allowed to import the Bifrost module (§4.4 seam: the engine interface lives at `internal/llm/engine/ifaces/`).
2. **Engine seam** — `internal/llm/engine/ifaces/Engine` interface so a future engine swap (e.g. an in-house router) is one driver away. Drivers self-register from `init()` and dispatch via `internal/llm/engine/engine.go::Open`. `cmd/portico` blank-imports the Bifrost driver — nothing else in production code imports it.
3. **OpenAI-compatible northbound API** — `/v1/chat/completions`, `/v1/completions`, `/v1/embeddings`, `/v1/moderations`, `/v1/models`, streaming and non-streaming. Authentication via the same JWT validator the MCP gateway uses (with new scopes: `llm:invoke`, `llm:embed`, `llm:moderate`).
4. **Provider registry — built-ins** — CRUD over LLM provider configurations whose `driver` is one of Bifrost's 23 native names (`openai`, `anthropic`, `azure`, `bedrock`, `cerebras`, `cohere`, `elevenlabs`, `fireworks`, `gemini`, `groq`, `huggingface`, `mistral`, `nebius`, `ollama`, `openrouter`, `parasail`, `perplexity`, `replicate`, `runway`, `sgl`, `vertex`, `vllm`, `xai`). Per-tenant. Each provider points at a vault entry for credentials. Endpoint, region, organisation id, default model are configurable.
5. **Provider registry — custom (OpenAI-compatible)** — operators can register a `driver: custom_openai` provider with a `base_url`, optional auth headers, and a model-name allowlist. This is how we cover Agent Gateway's `deepseek`, `httpbun` mock, and any regional / on-prem OpenAI-compatible endpoint the operator runs. The Bifrost adapter routes these through Bifrost's custom-provider Account hook (`GetConfigForProvider` returning a `ProviderConfig` with the operator's base URL). The Console offers a *Catalog* of preset custom-provider templates (DeepSeek, Together AI, Anyscale, Lepton, Lambda, internal vLLM, internal Ollama, …) — registering one is a single click that pre-fills `base_url` and the chat-completions path.
6. **Model registry** — operator names model aliases that map onto provider+model pairs. Tenant A's `gpt-4` can resolve to Azure OpenAI's `gpt-4-deployment-1`; Tenant B's `gpt-4` can resolve to OpenAI's `gpt-4o`. Aliases are first-class — clients refer to aliases, not provider models.
7. **Per-tenant key vaulting** — every provider API key lives in the Phase 5 vault, keyed by `(tenant, provider, key_purpose)`. Keys never leave the gateway. The Bifrost adapter implements Bifrost's `Account` interface so that `GetKeysForProvider` reads from vault on every request (no in-memory caching of plaintext beyond a per-request lifetime; Bifrost's weight-based routing across multiple keys is supported via `schemas.Key.Weight`).
8. **Tool-use bridging** — when a model emits an OpenAI tool call (`tool_calls` with name + arguments), Portico translates to MCP `tools/call` against the matching tool in the live catalog (subject to the policy engine + approval flow), feeds the tool result back into the model loop, and continues the conversation. The bridge respects MCP's elicitation flow (model-requested approval is queued the same way an external client's would be).
9. **Quotas + rate limits** — per-tenant token quotas (input/output/total per minute, per day) and request-per-minute caps. Defaults sane; configurable. Exceeded quotas return `429 quota_exceeded` with a typed error body. Per-tenant quotas in this phase are a Portico-side enforcer; Phase 15.5 layers a richer Virtual-Key + hierarchical-budget model on top.
10. **Observability** — every LLM call produces a span with attributes: `llm.engine` (= `bifrost`), `llm.provider`, `llm.provider_driver`, `llm.model`, `llm.alias`, `llm.prompt_tokens`, `llm.completion_tokens`, `llm.cost_usd` (when computable), `llm.tool_calls`. Spans land in the Phase 11 span store. Audit events: `llm.invoked`, `llm.streamed`, `llm.tool_bridged`, `llm.quota_exceeded`, `llm.failed`, `llm.fallback_used` (Bifrost selected a fallback key/provider), `llm.custom_provider_invoked`.
11. **Policy extension** — policy rules gain LLM-specific matchers (provider, provider_driver, model alias, prompt regex, max tokens) and actions (deny, require_approval, redirect_to_alternative_model).
12. **Console screens** — `/llm/providers` (separated tabs: *Built-in* and *Custom*), `/llm/models`, `/llm/quotas`, `/llm/sessions` (chat replay reusing Phase 11 inspector), an LLM playground at `/llm/playground` (similar shape to Phase 10's MCP playground but with chat and completion modes), `/llm/health` (per-provider live status — Bifrost surfaces this via its plugin hooks; we expose it on our REST + Console).
13. **Cost telemetry** — model unit costs configurable per provider; cost_usd computed per call (Bifrost surfaces token counts in its response envelope) and aggregated in `/llm/cost` dashboards (per tenant, per model, per day). Operators can set per-tenant budgets that emit warnings + denials at thresholds. Hierarchical budgets (VK / team / customer) land in Phase 15.5.
14. **Conformance suite extension** — `cmd/portico conformance --suite openai` runs OpenAI-API conformance against the live binary so a tenant can validate their integration.

## Acceptance criteria

1. Bifrost SDK pulls in cleanly. `CGO_ENABLED=0 go build` succeeds. Binary size delta vs. Phase 12 ≤ +25 MB (Bifrost's footprint is documented; we accept it).
2. OpenAI clients (Python `openai`, JS `openai`, `curl`) work end-to-end against `http://localhost:8080/v1/...` with a bearer token issued by Portico's JWT machinery.
3. Streaming responses (Server-Sent Events for chat completions) work. Backpressure-safe. Heartbeat comments every 15 s on long generations.
4. Model alias resolution: a request to `gpt-4` for tenant A reaches the configured provider+model; same alias for tenant B reaches a different provider+model. No cross-tenant leakage.
5. Vault key never visible in logs, audit, or error messages. A test deliberately injects a known token shape and asserts it never appears in any persistence layer except the encrypted vault.
6. Tool-use bridging: a model that returns a tool call (OpenAI format) for a tool that exists in the MCP catalog dispatches via the existing dispatcher, the result feeds back, the model continues, and the full transcript records every bridged call as a child span.
7. Quotas: a tenant exceeding its per-minute token cap receives `429 quota_exceeded`; the failed request is audit-logged; subsequent allowed requests work after the window.
8. Cost telemetry: 1000 known-shape calls produce a cost figure within 1% of the manually-computed total.
9. Policy: a deny rule on `model: gpt-4` blocks the request before the provider call; an audit event records the decision; provider key never used.
10. **Custom-provider parity.** A `custom_openai` provider configured against a local `httpbun`-shaped mock answers `/v1/chat/completions` end-to-end through Portico. A `custom_openai` provider configured against the DeepSeek API succeeds in a live test (skipped in CI; runnable from a developer machine with a real key).
11. **Engine seam.** Removing the blank import of the Bifrost engine driver from `cmd/portico/` causes the binary to fail to start with a `registered engines: [...]` error — proving the §4.4 seam holds. Production code outside `cmd/portico` and the driver's own tests never imports `github.com/maximhq/bifrost/...` (lint rule enforced — see §13 forbidden practices update below).
12. **Bifrost fallback respected.** When two keys are configured for one provider with different weights and one is force-invalidated mid-test, the second serves the request without operator intervention; a `llm.fallback_used` audit event records the swap.
13. Smoke: `scripts/smoke/phase-13.sh` covers providers CRUD (built-in + custom), models CRUD, chat completion (non-streaming + streaming), embeddings, tool-use bridging, custom-provider chat call, fallback path, quota exceedance. SKIP for unimplemented; OK ≥ 14 by phase close.
14. Coverage: ≥ 75% across new packages; ≥ 80% on `internal/llm/gateway`, `internal/llm/bridge`, `internal/llm/quota`, `internal/llm/engine/bifrost`.
15. Cross-tenant isolation: integration test asserts every isolation invariant from Phase 0 + every LLM-specific one (provider keys, model aliases, quotas, conversations, custom-provider configs).

## Architecture

```
internal/llm/
├── gateway/
│   ├── handler.go                # /v1/* endpoints
│   ├── streaming.go              # SSE framing
│   ├── auth.go                   # bearer extraction + JWT validation
│   └── handler_test.go
├── engine/
│   ├── ifaces/
│   │   └── engine.go             # Engine interface — the §4.4 seam for the inference engine
│   ├── engine.go                 # Open(name, cfg) factory + registry
│   └── bifrost/
│       ├── adapter.go            # wraps github.com/maximhq/bifrost/core
│       ├── account.go            # implements schemas.Account: GetConfiguredProviders / GetKeysForProvider / GetConfigForProvider
│       ├── custom_provider.go    # surfaces our "custom_openai" driver through Bifrost's custom-provider hook
│       ├── fallback.go           # wires Bifrost's weight + fallback knobs from our provider/model config
│       └── adapter_test.go
├── providers/
│   ├── provider.go               # Provider type — driver-agnostic Portico shape
│   ├── store.go                  # SQLite-backed registry (per tenant)
│   ├── catalog.go                # built-in templates for one-click custom-provider creation (DeepSeek, Together, Lepton, …)
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
│   ├── enforcer.go               # token + RPM enforcement (per-tenant; Phase 15.5 extends with VK/team/customer)
│   ├── window.go                 # rolling-window counter
│   └── enforcer_test.go
├── cost/
│   ├── calculator.go             # per-provider unit costs (seeded + per-tenant overrides)
│   ├── ledger.go                 # per-tenant cost rollups
│   └── calculator_test.go
└── policy_extensions.go          # LLM matcher/action support for the policy engine

internal/storage/sqlite/migrations/
├── 0014_llm_providers_models.sql
├── 0015_llm_quotas_costs.sql
└── 0016_llm_sessions.sql

internal/server/api/
├── llm_providers.go
├── llm_models.go
├── llm_quotas.go
├── llm_costs.go
└── llm_sessions.go               # chat session replay (reuses Phase 11 bundle shape)

cmd/portico/
├── cmd_conformance.go            # extended with --suite openai
└── llm_wiring.go                 # blank import of internal/llm/engine/bifrost — the only consumer of Bifrost
```

The northbound MCP gateway (Phase 1+) and the LLM gateway share the listener, the JWT validator, the audit machinery, and the span store. They diverge at the URL prefix (`/api/`, `/mcp/`, `/v1/`) and the dispatcher.

### Engine interface — the §4.4 seam

```go
// internal/llm/engine/ifaces/engine.go
package ifaces

type Engine interface {
    Name() string
    // ChatCompletion dispatches a fully-resolved request (provider + model already chosen by the registry)
    // to the underlying inference engine and returns either a non-streaming response or a stream channel.
    ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
    Embedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
    // ProvidersSupported declares the driver names this engine can route to. The provider registry
    // uses this for validation: rejecting a driver the engine cannot serve at config-load time.
    ProvidersSupported() []string
    // Health returns the engine's view of each provider (driver/key) it has been asked to use.
    Health(ctx context.Context) ([]ProviderHealth, error)
}

type Driver interface {
    Name() string
    New(cfg map[string]any, deps Deps) (Engine, error)
}

// Deps carries the cross-cutting services every engine driver may need.
type Deps struct {
    Logger    *slog.Logger
    Tracer    trace.Tracer
    Audit     audit.Emitter
    Vault     secrets.Vault
    Providers providers.Repo  // for fetching credentials at dispatch time
}

func Register(d Driver) { /* … */ }
```

### Bifrost adapter

The adapter is the only file that imports `github.com/maximhq/bifrost/core` and `github.com/maximhq/bifrost/core/schemas`. Its `account.go` implements the Bifrost `schemas.Account` interface so Bifrost can call back for keys and config on every dispatch — which means our Vault is the source of truth, never an in-memory snapshot.

```go
// internal/llm/engine/bifrost/adapter.go
package bifrost

import (
    "github.com/maximhq/bifrost/core"
    "github.com/maximhq/bifrost/core/schemas"
)

type adapter struct {
    client *core.Bifrost
    deps   ifaces.Deps
}

func (a *adapter) ChatCompletion(ctx context.Context, req *ifaces.ChatRequest) (*ifaces.ChatResponse, error) {
    // 1. Translate req → schemas.BifrostChatRequest, including resolved provider name.
    // 2. Apply policy / quota pre-checks (already done before reaching the engine in handler.go).
    // 3. Call a.client.ChatCompletionRequest(...).
    // 4. Translate response back, populate cost from response.Usage + cost.UnitCost lookup.
    // 5. Emit span attrs, audit event, ledger entry.
}

func (a *adapter) buildAccount(tenantID string) schemas.Account {
    return &porticoAccount{deps: a.deps, tenantID: tenantID}
}
```

```go
// internal/llm/engine/bifrost/account.go
type porticoAccount struct {
    deps     ifaces.Deps
    tenantID string
}

func (p *porticoAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
    // Read providers from Portico's registry filtered by tenant. Map driver -> Bifrost ModelProvider.
}

func (p *porticoAccount) GetKeysForProvider(provider schemas.ModelProvider) ([]schemas.Key, error) {
    // Read all enabled credential rows for (tenant, provider) from our store; for each row,
    // fetch the secret from Vault on every call (no plaintext caching beyond this scope).
    // Map row.Weight -> schemas.Key.Weight to drive Bifrost's weighted routing + fallback.
}

func (p *porticoAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
    // Read per-(tenant, provider) network/concurrency knobs (RPS caps, retry, timeouts).
    // For driver="custom_openai", inject BaseURL, headers, chat path, and model whitelist
    // into the schemas.ProviderConfig — this is how custom providers ride Bifrost's existing
    // OpenAI-compatible code path with no engine fork.
}
```

### Custom-provider story — closing the Agent Gateway gap

Agent Gateway's openai-compatible list (Mistral, DeepSeek, Ollama, vLLM) is mostly already in Bifrost natively, except **DeepSeek** and any operator-specific OpenAI-compatible endpoint (regional/enterprise mirrors, internal vLLM clusters, self-hosted Ollama at a non-default port, `httpbun`-style mocks for tests). We expose all of those through a single `driver: custom_openai` slot — registered in `internal/llm/providers/catalog.go` with curated templates so the operator clicks "DeepSeek" or "Together" or "Internal vLLM" and gets the right `base_url`, `chat_path`, and default header set.

Templates ship for: DeepSeek, Together AI, Anyscale, Lepton, Lambda, Fireworks (also native — both work), Replicate (also native), Perplexity (also native), `httpbun` test mock, and an "Other" stub the operator fills in by hand. Templates are documented in `docs/concepts/llm-providers.md`.

This means: **after Phase 13, Portico exposes a strict superset of Agent Gateway's model surface, without copying their adapter code, by combining Bifrost's 23 native drivers with our custom-provider templates.**

## SQL DDL

> **As-built note (2026-06-16).** Migration numbers below are corrected from the original
> draft: `0013` was already taken by `0013_imported_sessions.sql` (Phase 11), so the LLM
> migrations land at **0014 / 0015 / 0016**. Also, per `CLAUDE.md` §4.4, the stores live in
> `internal/storage/{ifaces,sqlite}` (domain types + `LLMProviderStore`/`LLMModelStore`
> interfaces in `ifaces/`, SQLite impls in `sqlite/<feature>_store.go`, exposed via `*DB`
> accessor methods) — not in `internal/llm/providers/store.go` as the package tree above
> sketches. The `internal/llm/...` tree still owns the engine/gateway/bridge logic.

### Migration 0014 — providers + models

```sql
CREATE TABLE IF NOT EXISTS tenant_llm_providers (
    tenant_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    driver        TEXT NOT NULL,            -- one of Bifrost's 23 native names OR 'custom_openai'
    config_json   TEXT NOT NULL,            -- driver-specific (endpoint, region, org_id, base_url, headers, …)
    credential_ref TEXT,                    -- vault key
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, name)
);

-- One provider may have N keys (weighted routing + fallback per Bifrost's Key.Weight).
CREATE TABLE IF NOT EXISTS tenant_llm_provider_keys (
    tenant_id     TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    key_id        TEXT NOT NULL,            -- ULID
    credential_ref TEXT NOT NULL,           -- vault entry holding the secret
    weight        REAL NOT NULL DEFAULT 1.0,
    model_allowlist TEXT NOT NULL DEFAULT '[]', -- JSON array; empty = all models
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, provider_name, key_id),
    FOREIGN KEY (tenant_id, provider_name) REFERENCES tenant_llm_providers(tenant_id, name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tenant_llm_models (
    tenant_id     TEXT NOT NULL,
    alias         TEXT NOT NULL,            -- e.g. 'gpt-4', 'fast-summary'
    provider_name TEXT NOT NULL,            -- references tenant_llm_providers
    provider_model TEXT NOT NULL,           -- e.g. 'gpt-4o', 'claude-3-5-sonnet-20241022', 'deepseek-chat'
    default_params_json TEXT NOT NULL DEFAULT '{}', -- temperature, top_p, max_tokens
    capabilities  TEXT NOT NULL DEFAULT '[]', -- JSON array: 'chat'|'completion'|'embedding'|'moderation'|'tool_use'
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, alias),
    FOREIGN KEY (tenant_id, provider_name) REFERENCES tenant_llm_providers(tenant_id, name)
);
```

### Migration 0015 — quotas + costs

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
    provider_driver TEXT NOT NULL,         -- 'openai', 'anthropic', 'custom_openai', …
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

### Migration 0016 — chat sessions

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
    Driver        string        // Bifrost native name OR 'custom_openai'
    Config        map[string]any
    CredentialRef string        // default key; multi-key cases live in tenant_llm_provider_keys
    Enabled       bool
}

type Repo interface {
    List(ctx context.Context, tenantID string) ([]Provider, error)
    Get(ctx context.Context, tenantID, name string) (*Provider, error)
    Put(ctx context.Context, tenantID string, p Provider) error
    Delete(ctx context.Context, tenantID, name string) error

    ListKeys(ctx context.Context, tenantID, providerName string) ([]ProviderKey, error)
    PutKey(ctx context.Context, tenantID, providerName string, k ProviderKey) error
    DeleteKey(ctx context.Context, tenantID, providerName, keyID string) error
}

type ProviderKey struct {
    ID             string
    CredentialRef  string
    Weight         float64
    ModelAllowlist []string
    Enabled        bool
}
```

```go
// internal/llm/providers/catalog.go

type Template struct {
    Slug        string  // "deepseek", "together", "lepton", "vllm-internal", "httpbun-mock"
    Label       string
    Driver      string  // typically "custom_openai"
    Description string
    Defaults    map[string]any   // base_url, chat_path, headers, default models
    Notes       string
}

func Templates() []Template
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
```

(Other request types unchanged from the previous draft — `ChatMessage`, `ToolCall`, `FunctionCall`, `Enforcer`, `UnitCost`, etc.)

```go
// internal/llm/bridge/tool_bridge.go — unchanged from the prior draft

func ToOpenAITools(ctx context.Context, tenantID, sessionID string) ([]gateway.ToolDef, error)
func HandleToolCall(ctx context.Context, tenantID, sessionID string, call gateway.ToolCall) (gateway.ChatMessage, error)
```

## REST API

OpenAI-compatible (unchanged):

```
POST   /v1/chat/completions                # streamed via stream=true
POST   /v1/completions
POST   /v1/embeddings
POST   /v1/moderations
GET    /v1/models                          # tenant's enabled aliases
GET    /v1/models/{alias}                  # detail
```

Portico-native admin surface (new endpoints in **bold**):

```
GET    /api/llm/providers
POST   /api/llm/providers
GET    /api/llm/providers/{name}
PUT    /api/llm/providers/{name}
DELETE /api/llm/providers/{name}

GET    /api/llm/providers/{name}/keys                # provider key roster (weighted routing)
POST   /api/llm/providers/{name}/keys
DELETE /api/llm/providers/{name}/keys/{key_id}

**GET    /api/llm/providers/templates**              # custom-provider one-click catalog
**POST   /api/llm/providers/from-template**          # create a provider from a catalog template
**GET    /api/llm/health**                           # per-(provider, key) live status from the engine

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

Errors uniform with `docs/plans/README.md`. New typed slugs: `quota_exceeded`, `provider_unavailable`, `model_unknown`, `unsupported_capability`, `policy_denied_llm`, `custom_provider_invalid`, `engine_unavailable`.

## CLI

```bash
portico llm providers list|get|put|delete
portico llm providers keys list|add|remove --provider <name>
portico llm providers from-template --template deepseek
portico llm providers templates                 # list catalog templates
portico llm models list|get|put|delete
portico llm quotas get|put
portico llm costs --day YYYY-MM-DD
portico llm health                               # mirrors GET /api/llm/health

# Conformance against OpenAI surface
portico conformance --suite openai --target http://localhost:8080 --token "$JWT"
```

## Console screens

### `/llm/providers`

Two tabs: **Built-in** and **Custom**.

- **Built-in tab** — `Table`: name, driver, endpoint, status, key count. "+ Add" opens a Modal with a driver picker over Bifrost's 23 native names; driver-specific fields render below (Azure deployment, Bedrock region, Vertex project, …). Credential picker dropdown (Phase 9 vault). "Add another key" button surfaces the weighted-routing primitive: a single provider may have multiple keys with different weights and model allowlists. "Test connectivity" button issues a no-op call (e.g. list-models) and reports success/failure.
- **Custom tab** — same `Table` shape, but `+ Add` opens a Catalog picker (`Card` grid: DeepSeek, Together, Anyscale, Lepton, Lambda, Internal vLLM, Internal Ollama, httpbun mock, Other). Selecting a card pre-fills `base_url`, `chat_path`, default headers; the operator only supplies the credential and (optionally) a model allowlist. "Other" exposes the bare schema for an arbitrary OpenAI-compatible endpoint.

### `/llm/models`

`Table`: alias, provider, provider_model, capabilities, enabled. Add via Modal: alias text, provider picker, provider model dropdown (lazy-loaded from the provider's `/models` API), capability checkboxes, default params editor.

### `/llm/quotas`

Form: per-tenant quotas. Live indicator showing current minute's usage as a percentage bar. "Reset minute counter" admin action behind approval. (Phase 15.5 adds the VK / team / customer hierarchy on top of this.)

### `/llm/costs`

Two views:

- **By day** — line chart (svg, no charting library) of cost_usd over the last 30 days.
- **By model** — `Table` aggregated for a date or range; export CSV.

### `/llm/playground`

Layout similar to Phase 10's MCP playground:

- Left: alias picker + capability filter.
- Centre: chat composer (multi-turn). System message, user messages, optional tool selection (drawn from the live MCP catalog so tool-use bridging is testable here).
- Right: span tree + audit + cost + tokens + provider/key chosen — all live. When Bifrost picks a fallback key, the right panel surfaces that and links to the audit event.

A "Save as case" button is reused from Phase 10. Replay reruns the conversation against the current providers (cost may differ; the replay UI surfaces the delta).

### `/llm/health`

`Table` of (provider, key, last call, status, success rate over 5m, last error). Rows go red when Bifrost reports the key as failing; click to drill into the most recent failure span. This surface is built on the engine's `Health()` call — no Bifrost-private state exposed; the engine adapter translates Bifrost's plugin-hook data into our shape.

### `/llm/sessions`

`Table` of chat sessions with role + token + cost summaries. Click → transcript view (read-only) + "Replay" CTA (Phase 11 inspector pattern).

## Implementation walkthrough

### Step 1 — Migrations + repos

Land migrations 0013–0015. Implement provider + provider-key + model + quota + cost repos. Round-trip tests cover canonical-JSON round trip on `config_json`, `default_params_json`, etc., and the multi-key invariants.

### Step 2 — Engine seam + Bifrost adapter

Define `internal/llm/engine/ifaces.Engine`. Implement the Bifrost adapter under `internal/llm/engine/bifrost/`:

- `adapter.go` — adapter struct, lifecycle, request translation.
- `account.go` — `schemas.Account` implementation backed by the provider repo + vault.
- `custom_provider.go` — `GetConfigForProvider` returns a `ProviderConfig` with operator-supplied `BaseURL` etc. for `driver=custom_openai`.
- `fallback.go` — translates per-key `weight` + per-provider `fallback_to` config into Bifrost's routing knobs.

The adapter self-registers via `init()`. The factory in `engine.go` dispatches by name. The error message lists registered engines (per §4.4 reference).

`cmd/portico/llm_wiring.go` blank-imports the Bifrost driver — the only consumer of Bifrost in the entire production tree.

### Step 3 — Provider catalog (custom-provider templates)

`internal/llm/providers/catalog.go` defines a curated list of templates. Each template has a slug, label, description, driver name (`custom_openai`), and default config (`base_url`, `chat_path`, header set). Templates are exposed via REST (`GET /api/llm/providers/templates`) and consumed by the Console Custom tab.

Initial template set: DeepSeek, Together AI, Anyscale, Lepton, Lambda, Internal vLLM (placeholder), Internal Ollama (placeholder), httpbun mock.

### Step 4 — Northbound handler

`internal/llm/gateway/handler.go` parses OpenAI-shaped requests, validates the JWT, resolves the alias, evaluates policy, checks the quota, dispatches via the Engine interface, records cost, emits audit + span. Streaming path uses the existing SSE framing helpers from Phase 1's northbound transport. The handler is engine-agnostic — it only sees `engine.Engine`.

### Step 5 — Tool-use bridge

Unchanged from the prior draft — the bridge dispatches via `internal/mcp/dispatcher` (Phase 1) regardless of engine. Approval, redaction, audit all apply.

### Step 6 — Quota enforcer

Unchanged from the prior draft (rolling-window counters per tenant). Pre-call estimate from prompt token count; post-call reconciliation. Phase 15.5 layers the VK + hierarchical-budget model on top of this.

### Step 7 — Cost calculator + ledger

Unit costs seeded from a curated table (`internal/llm/cost/seeds.go`) covering all 23 native Bifrost providers + the common custom-provider models (DeepSeek `deepseek-chat`, `deepseek-coder`; Together's Llama variants; Anyscale's; …). Operators can override per-deployment via `POST /api/llm/costs/units`. Per-call costs roll up into `tenant_llm_cost_daily`.

### Step 8 — Policy extension

Unchanged from the prior draft. Matchers: `provider`, `provider_driver`, `model_alias`, `prompt_regex`, `max_tokens_gt`. Actions: `deny`, `require_approval`, `redirect_to_alias`.

### Step 9 — Console screens

CRUD pages follow the Phase 9 pattern. Custom-provider tab uses the catalog endpoint to drive its `Card` grid. Playground reuses Phase 10 components. Health page is a thin read-only `Table` over `GET /api/llm/health`. Cost dashboards use SVG; no charting library.

### Step 10 — OpenAI conformance suite

`internal/conformance/tests/openai/*.go` adds tests:

- `chat/completions` round-trip + streaming.
- `tool_calls` round-trip.
- `embeddings`.
- `models/list` returns aliases.
- Error surfaces (model_unknown, quota_exceeded, policy_denied_llm, custom_provider_invalid).
- Streaming heartbeat.

### Step 11 — Smoke + tests

`scripts/smoke/phase-13.sh` covers built-in + custom providers CRUD, models CRUD, chat completion (non-streaming + streaming), embeddings, tool-bridging happy path, custom-provider call (against an in-process `httpbun`-shaped mock), fallback path (kill one key, confirm second serves), quota exceedance, health endpoint, conformance summary.

## Test plan

### Unit

- `internal/llm/providers/store_test.go` — CRUD + key roster + tenant isolation + invalid driver rejected.
- `internal/llm/providers/catalog_test.go` — every template renders to a valid `Provider` config.
- `internal/llm/models/store_test.go` — alias resolution + capability filter + missing provider rejected.
- `internal/llm/engine/engine_test.go` — factory lists registered engines; unknown engine error message includes the registry.
- `internal/llm/engine/bifrost/adapter_test.go`
  - `TestAdapter_ChatCompletion_NonStreaming`.
  - `TestAdapter_ChatCompletion_Streaming_OrderedChunks`.
  - `TestAdapter_Embedding_HappyPath`.
  - `TestAdapter_CustomProvider_RoutesToBaseURL`.
  - `TestAdapter_Fallback_WhenPrimaryKeyFails`.
  - `TestAdapter_VaultKey_FetchedPerCall`.
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
- `TestE2E_TenantIsolation_Comprehensive` — every LLM table including provider keys + custom providers.
- `TestE2E_CustomProvider_OpenAICompat` — register a `custom_openai` provider against an in-process OpenAI-shaped mock; call `/v1/chat/completions`; assert the upstream's URL was hit with the right headers.
- `TestE2E_BifrostFallback_OnFirstKeyFailure`.
- `TestE2E_EngineSeam_RemovingBlankImportFailsBoot` — boot a binary built without the Bifrost driver; assert `registered engines: []` error.

### Frontend tests

- Playwright: built-in provider create + test connectivity, custom-provider catalog flow (pick DeepSeek template → fill credential → save → see green status), model alias create, playground chat happy path, cost dashboard renders, policy redirect respected, health page shows green/red status when a key is force-disabled.

### Smoke

`scripts/smoke/phase-13.sh`:
- POST `/api/llm/providers` (built-in, mock provider with httptest backing) → 201.
- POST `/api/llm/providers/from-template` (slug=httpbun-mock) → 201.
- POST `/api/llm/providers/{name}/keys` → 201 (second key with weight 0.5).
- POST `/api/llm/models` → 201.
- POST `/v1/chat/completions` (built-in) → 200 + valid OpenAI shape.
- POST `/v1/chat/completions` (custom_openai) → 200 + valid OpenAI shape.
- POST `/v1/chat/completions` with `stream: true` → SSE chunks ordered + final `[DONE]`.
- POST `/v1/embeddings` → 200.
- GET `/api/llm/health` → 200 + non-empty body.
- Force-disable one key; POST `/v1/chat/completions` again → 200 (fallback served by second key); audit event `llm.fallback_used` present.
- GET `/api/llm/costs?day=…` → 200.
- POST `/v1/chat/completions` past quota → 429 + `quota_exceeded` slug.

OK ≥ 14 by phase close, FAIL = 0.

### Coverage gates

- `internal/llm/gateway`: ≥ 80%.
- `internal/llm/engine/bifrost`: ≥ 80%.
- `internal/llm/bridge`: ≥ 80%.
- `internal/llm/quota`: ≥ 80%.
- `internal/llm/cost`: ≥ 75%.
- `internal/llm/providers`, `internal/llm/models`: ≥ 75%.

## Common pitfalls

- **Bifrost dependency creep.** Vet Bifrost's dependency graph — pin its release tag, check for CGo, run a fresh `go.sum` audit. Bifrost itself is pure-Go, but any new transitive that breaks `CGO_ENABLED=0` is a blocker.
- **Engine seam violations.** Importing `github.com/maximhq/bifrost/...` from anywhere except `internal/llm/engine/bifrost/...` or `cmd/portico/llm_wiring.go` is a §13 forbidden practice. The lint config gains an `import` deny rule for the Bifrost path scoped to those locations.
- **Provider key in error messages.** Some upstream errors echo the request verbatim. Wrap every provider error (including ones surfaced by Bifrost) with a redactor that scrubs known-shape tokens (`sk-[A-Za-z0-9]{32,}`, `xai-…`, `anthropic-…`, etc.) before bubbling.
- **Streaming back-pressure.** A slow client receiving SSE can stall the response goroutine. Use a per-connection bounded channel + drop with `audit.dropped` event on overflow; never block the upstream.
- **Tool-use loops without cap.** A model that keeps requesting tools without progress chews tokens + cost. Loop cap mandatory; emit `llm.tool_loop_exceeded` audit when hit.
- **Bifrost's "HTTP-mode" admin surface mixed up with ours.** Bifrost can run as an HTTP service with its own Console + REST. We do **not** expose that; we use Bifrost as a Go library. Any code that reaches for Bifrost's `transports/http` package is wrong — the surface is our `internal/server/api` + Console.
- **Vault key caching past one call.** Bifrost asks for keys at dispatch time. We fetch from Vault inside `GetKeysForProvider`. Never short-circuit that with an in-memory cache that lasts past the request scope (Phase 5 sets the trust-zone boundary).
- **Custom-provider "Other" template trust.** The "Other" template lets the operator point at an arbitrary base URL. Document clearly that this is a credentialed egress channel; policy rules on `provider_driver=custom_openai` are how operators constrain which models reach it.
- **Cross-provider response shape drift.** Bifrost normalises the OpenAI surface, but a stray provider-specific field can leak. Run a JSON Schema check on every outbound payload against the OpenAI response shape.
- **Quota races.** Two simultaneous requests can both pass the pre-check and both push the counter over. Counter increment must use atomic SQL `UPDATE … SET v = v + ? WHERE …` and the pre-check must read the row in the same transaction.
- **Cost rounding drift.** Floating-point cost computation accumulates error. Round to 6 decimal places at write; tests assert sums are exact for known inputs.
- **Replay against deprecated models.** A saved chat session for a model the provider has deprecated will fail. The replay UI surfaces "model unavailable" with a "remap to alternative" CTA.
- **Audit payload bloat.** Full prompt + response per call is huge. Audit stores a redacted *summary* (first 256 chars + token counts + tool call summaries); the full content lands in `tenant_llm_messages` and is gated behind a separate scope.
- **Policy on prompt content.** Regex on prompts is fragile and bypassable. Document this clearly: prompt-content rules are advisory; deny by model+tool+capability is the sturdy enforcement layer.
- **OpenAI client version drift.** OpenAI evolves the API; some clients send fields Bifrost may not yet support. Reject unknown fields with a typed error rather than silently dropping them.
- **Multi-region keys.** A tenant with US and EU keys for the same provider needs either two provider entries (one per region) or one provider with two keys whose `model_allowlist` partitions traffic. Document both patterns.

## Out of scope

- **Fine-tuning APIs.** `/v1/fine_tuning/*` — post-V1.5.
- **Files API + assistants API.** Out of scope; doesn't fit the gateway model.
- **Streaming embeddings.** The OpenAI surface doesn't stream embeddings; we don't either.
- **Function-calling fall-back for non-tool-use models.** If a model doesn't support tool use, the request includes tools but the model can't use them. We surface `unsupported_capability`; we do not synthesise tool use via prompt engineering.
- **Hosted SaaS variant.** Per RFC §15.
- **Per-user (sub-tenant) cost attribution.** Cost is per tenant; per-user is post-V1.5. (Phase 15.5 brings VK-level attribution.)
- **Custom embedding indices.** Vector store integration is a different product surface. (Phase 15.5 adds a vector store, but only for semantic caching — not as a tenant-facing index.)
- **Image / audio modalities.** Phase 13 ships text only; multimodal is a follow-up. Bifrost natively supports image / TTS / STT / video — surfacing those is a future phase.
- **Bifrost's HTTP-mode Console.** We re-use the engine, not the admin surface.
- **MCP Code Mode integration.** That's Phase 13.5.
- **Semantic caching + Virtual Keys + hierarchical budgets.** That's Phase 15.5.

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-13.sh` shows OK ≥ 14, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. `portico conformance --suite openai` against `portico dev` passes 100%.
5. Bundle size + image size deltas documented in the PR description.
6. Docs site gains `/docs/concepts/llm-gateway`, `/docs/concepts/llm-providers` (including the custom-provider template catalog), and how-tos for adding a provider, registering a model, setting quotas, debugging a chat session.
7. CHANGELOG updated with V1.5 entry. RFC-001 §11.2 (allowed library surface) updated to include Bifrost with rationale.
8. `AGENTS.md` §13 forbidden practices updated: "Importing `github.com/maximhq/bifrost/...` from anywhere except `internal/llm/engine/bifrost/...` and `cmd/portico/llm_wiring.go`."
9. `docs/plans/README.md` index lifts Phase 13's status from planned to landed.

## Hand-off to Phase 13.5 and 15.5

Phase 13 leaves the LLM gateway operational with the engine seam in place. Two near-term follow-ups exploit that surface:

- **Phase 13.5 — MCP Code Mode.** A Portico-native reimplementation of Bifrost's Code Mode pattern (four meta-tools + Starlark sandbox), bound to our existing Skills runtime + snapshot model. Sits in the MCP path; uses the LLM path only as a consumer.
- **Phase 15.5 — Semantic Cache + Virtual Keys.** A Bifrost-shaped governance layer on top of the LLM gateway: vector-backed semantic cache (Weaviate/Redis/Qdrant pluggable via the §4.4 seam) and hierarchical Virtual Keys / Teams / Customers with independent budgets. This is what makes Portico's LLM gateway *also* a cost-control plane, not just a pass-through.

The stable invariants: multi-tenant from V1, headless approval, credentials behind the gateway. Phase 13 inherits all three; subsequent phases must too.
