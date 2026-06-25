# LLM Gateway overview

Portico includes a full LLM gateway alongside its MCP gateway, served from the same binary, the same listener, and the same tenant identity. Agents that already get tool surfaces from Portico via MCP can now point their model calls at Portico as well, getting governed model access and governed tool access from a single base URL.

The northbound surface is OpenAI-compatible: any client library that speaks OpenAI's HTTP API works without modification. Southbound, Portico routes to whichever providers the operator configures — native built-in drivers cover nineteen upstream providers; an additional `custom_openai` driver covers any OpenAI-compatible endpoint the operator runs.

The inference layer is powered by a pure-Go, Apache-2.0 LLM engine embedded directly into the Portico binary. There is no sidecar process and no external service dependency. The engine is isolated behind a well-defined interface (`internal/llm/engine/ifaces.Engine`) so the binary stays statically linkable with `CGO_ENABLED=0`.

## Architecture

```
AI client (openai SDK / curl / any HTTP client)
         |
         |  Bearer token (JWT, or Virtual Key in Phase 15.5)
         v
 ┌───────────────────────────────────────────────────────────────┐
 │  Portico — one listener (127.0.0.1:8080 default)             │
 │                                                               │
 │  /v1/*          LLM northbound handler                        │
 │    │  1. Validate JWT; extract tenant_id                      │
 │    │  2. Resolve model alias → provider + provider_model      │
 │    │  3. Policy check (provider / model / prompt constraints) │
 │    │  4. Quota pre-check (RPM / TPM / TPD)                   │
 │    │  5. Semantic cache lookup (Phase 15.5)                   │
 │    │  6. Dispatch via Engine interface                        │
 │    │  7. Record usage; update cost ledger                     │
 │    │  8. Audit event + span                                   │
 │    v                                                          │
 │  Engine seam (internal/llm/engine/ifaces.Engine)             │
 │    │                                                          │
 │    v                                                          │
 │  Embedded LLM engine (pure-Go, Apache-2.0)                   │
 │    │  — holds one per-tenant client, initialised lazily       │
 │    │  — fetches API keys from Vault on every dispatch         │
 │    │  — weighted multi-key routing + automatic fallback       │
 │    v                                                          │
 │  Upstream providers (OpenAI, Anthropic, Azure, Bedrock, …)   │
 └───────────────────────────────────────────────────────────────┘
```

The MCP gateway (`/mcp/`) and LLM gateway (`/v1/`) share the HTTP listener, the JWT validator, the audit pipeline, and the OpenTelemetry span store. They diverge only at the URL prefix and their respective dispatchers.

## OpenAI-compatible northbound

The following endpoints accept requests from any OpenAI-compatible client:

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | Chat completion, streaming or non-streaming |
| `POST` | `/v1/completions` | Legacy text completion |
| `POST` | `/v1/embeddings` | Text embeddings |
| `POST` | `/v1/moderations` | Content moderation |
| `GET`  | `/v1/models` | List the tenant's enabled model aliases |
| `GET`  | `/v1/models/{alias}` | Detail for a specific alias |

Authentication uses the same `Authorization: Bearer <token>` header the MCP gateway uses. The JWT must include the appropriate scope:

| Operation | Required scope |
|-----------|----------------|
| Chat / completion | `llm:invoke` |
| Embeddings | `llm:embed` |
| Moderations | `llm:moderate` |

### Chat completion request shape

The `POST /v1/chat/completions` body follows the OpenAI schema. The `model` field carries a **model alias** defined in the tenant's registry, not a provider-specific model identifier.

```json
{
  "model": "gpt-4",
  "messages": [
    { "role": "system", "content": "You are a helpful assistant." },
    { "role": "user",   "content": "Summarise the transcript below." }
  ],
  "temperature": 0.3,
  "max_tokens": 512,
  "stream": false,
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "fetch_transcript",
        "description": "Fetch a meeting transcript by ID",
        "parameters": {
          "type": "object",
          "properties": { "id": { "type": "string" } },
          "required": ["id"]
        }
      }
    }
  ]
}
```

A `202 OK` non-streaming response returns an OpenAI-shaped object. Setting `"stream": true` returns Server-Sent Events with delta chunks and a terminal `data: [DONE]` line. The SSE framing is backpressure-safe; a heartbeat comment is emitted every 15 seconds during long generations to keep proxies alive.

::: info Drop-in replacement
Because the northbound is OpenAI-compatible, changing an existing integration to route through Portico is a one-line base URL change. No request-shaping code changes are needed. `GET /v1/models` returns only the aliases the tenant has configured, so model discovery also works.
:::

## Per-tenant provider registry

Each tenant maintains its own set of LLM providers. A provider row records:

- **`name`** — an operator-chosen label (e.g. `openai-prod`, `bedrock-us-east-1`).
- **`driver`** — the underlying protocol driver (see built-in list below).
- **`config_json`** — driver-specific fields such as `base_url`, `endpoint`, `region`, or `headers`.
- **`credential_ref`** — a reference into the tenant's vault for the default API key.
- **`enabled`** — whether the provider is active.

One provider may carry multiple keys (the key roster at `tenant_llm_provider_keys`). Each key has a `weight` and an optional `model_allowlist` (a JSON array of model strings; empty means all models). The embedded engine uses these weights to distribute load across keys and to fall back automatically when one key fails.

### Built-in drivers

The following driver names are natively understood by the embedded engine:

```
openai       anthropic    azure        bedrock      cohere
vertex       mistral      ollama       groq         sgl
parasail     perplexity   cerebras     gemini       openrouter
elevenlabs   huggingface  nebius       xai
```

Register a built-in provider by setting `driver` to one of these names.

### Custom OpenAI-compatible providers

Setting `driver: custom_openai` lets the operator point at any OpenAI-compatible endpoint: an on-premises inference cluster, a regional mirror, a self-hosted model server, or a test harness. The engine routes these through the same OpenAI code path, injecting the operator-supplied `base_url` and optional `headers`.

A curated **template catalog** is exposed at `GET /api/llm/providers/templates`. Templates pre-fill `base_url`, `chat_path`, and sensible defaults for common configurations. To create a provider from a template:

```bash
curl -s -X POST http://localhost:8080/api/llm/providers/from-template \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"slug": "deepseek", "name": "deepseek-prod", "credential_ref": "deepseek-key"}'
```

## Per-tenant model aliases

A model alias decouples the name clients use (`"gpt-4"`, `"fast-summary"`) from the provider and model identifier that actually receives the request. The same alias can resolve to different providers for different tenants.

```json
{
  "alias": "gpt-4",
  "provider_name": "openai-prod",
  "provider_model": "gpt-4o",
  "capabilities": ["chat", "tool_use"],
  "default_params": { "temperature": 0.7 }
}
```

Tenant A's `gpt-4` might resolve to an Azure deployment; Tenant B's `gpt-4` to a direct OpenAI key. Portico enforces this separation at the storage layer: every query against `tenant_llm_models` filters by `tenant_id`.

The `capabilities` array gates which endpoint types are valid for an alias. Requesting an embedding against an alias that only declares `chat` returns `unsupported_capability`.

## Vault-backed key management

Provider API keys never appear in `portico.yaml`, in logs, or in audit events. Each provider row carries a `credential_ref` pointing to an entry in the Phase 5 vault, keyed by `(tenant, name)`. The embedded engine resolves that reference on **every dispatch** — there is no in-process plaintext cache that outlives a single request. This boundary is enforced by the engine driver in `internal/llm/engine/`:

```go
// GetKeysForProvider resolves API keys for every enabled provider with the given
// driver, dereferencing each credential against the tenant's vault on every call
// (no plaintext caching beyond this scope).
func (p *porticoAccount) GetKeysForProvider(ctx context.Context, providerKey schemas.ModelProvider) ([]schemas.Key, error) {
    // ... reads from vault; returns []schemas.Key with Weight fields
}
```

Multi-key configurations support weighted load distribution. When the primary key fails (rate-limited, revoked, or returning upstream errors), the engine automatically selects the next eligible key by weight. An `llm.fallback_used` audit event records every such switch.

See [Credentials vault](/concepts/credentials-vault) and [Virtual Keys](/concepts/virtual-keys) for the complete credential lifecycle.

## Tool-use bridging

When a model returns a tool call in OpenAI's `tool_calls` format, Portico intercepts it before streaming the result back to the client. The bridge translates the OpenAI tool call into an MCP `tools/call` request, dispatches it through the live MCP catalog (subject to the policy engine and the approval flow), injects the result as a `tool` role message, and continues the inference loop.

This means:

- Every MCP tool the tenant has registered is available as an OpenAI-formatted tool in the LLM gateway — without the operator declaring the tools twice.
- Tool-call approval (`requires_approval` policy rules) applies identically whether the call originates from a direct MCP client or from inside an LLM loop.
- The full conversation transcript — including every bridged tool call and its result — lands as child spans linked to the parent inference span.

A configurable loop cap prevents runaway tool-call chains. When the cap is reached, an `llm.tool_loop_exceeded` audit event is emitted and the model loop terminates with an error.

## Quotas and rate limits

Per-tenant limits are enforced by a rolling-window quota enforcer (`internal/llm/quota.Enforcer`). The default limits applied to any tenant that has not configured its own are:

| Dimension | Default |
|-----------|---------|
| Requests per minute | 600 |
| Tokens per minute | 200,000 |
| Tokens per day | 4,000,000 |
| Cost (USD) per day | $100.00 |

These defaults are defined in `internal/storage/ifaces.DefaultLLMQuota`. Operators can override them per tenant:

```bash
curl -s -X PUT http://localhost:8080/api/llm/quotas \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "requests_per_minute": 1200,
    "tokens_per_minute": 400000,
    "tokens_per_day": 8000000,
    "cost_usd_per_day": 200.0
  }'
```

A request that would exceed the limit receives a `429` with a typed error body:

```json
{ "error": { "code": "quota_exceeded", "message": "llm quota exceeded: tokens_per_minute" } }
```

The quota enforcer tracks three separate dimensions (`requests_per_minute`, `tokens_per_minute`, `tokens_per_day`). The pre-call check runs before the provider is contacted; the post-call reconciliation records actual token counts from the response. Both use atomic window operations so concurrent requests cannot race past a limit.

For per-Virtual-Key and hierarchical budget enforcement, see [Hierarchical budgets](/concepts/hierarchical-budgets).

## Cost telemetry

The gateway computes a `cost_usd` figure for every call based on per-provider unit costs seeded from a curated table (`internal/llm/cost/seeds.go`). Operators can override unit costs per deployment. Cost rolls up into a per-tenant daily ledger (`tenant_llm_cost_daily`), queryable through:

```
GET /api/llm/costs?day=2026-06-24
GET /api/llm/costs/by-day?from=2026-06-01&to=2026-06-24
GET /api/llm/costs/by-model?day=2026-06-24
```

Every LLM call also produces an OpenTelemetry span with these attributes: `llm.provider`, `llm.provider_driver`, `llm.model`, `llm.alias`, `llm.prompt_tokens`, `llm.completion_tokens`, `llm.cost_usd`, and `llm.tool_calls`. Spans integrate with the same span store and bundle export used by the MCP gateway (see [Observability](/concepts/observability)).

## Semantic cache

The LLM gateway supports an optional semantic cache layer in front of every inference call (configured in Phase 15.5). When enabled, a cache hit returns the stored response immediately without contacting the provider, reducing both latency and cost.

Two matching modes are supported:

- **`exact`** — matches on a hash of the normalized request. Suited for deterministic prompts.
- **`semantic`** — matches on embedding similarity above a configurable threshold. Suited for natural-language queries where phrasing varies but intent is the same.

Cache scope controls the sharing boundary within a tenant: `tenant` (shared), `customer`, `team`, or `vk` (per Virtual Key). Cross-tenant sharing is structurally impossible — `tenant_id` is always the leading component of every cache key.

Configure the cache in `portico.yaml`:

```yaml
cache:
  driver: redis          # none | inmem | redis
  scope: tenant
  ttl: 10m
  threshold: 0.88        # semantic similarity floor; ignored in exact mode
  options:
    addr: "redis:6379"
    db: 0
```

See [Semantic cache](/concepts/semantic-cache) for tuning guidance, invalidation, and per-alias TTL overrides.

## Policy and governance

LLM calls flow through the same policy engine as MCP tool calls. Policy rules gain LLM-specific matchers:

| Matcher | Description |
|---------|-------------|
| `provider` | Named provider (e.g. `openai-prod`) |
| `provider_driver` | Driver type (e.g. `anthropic`, `custom_openai`) |
| `model_alias` | Model alias (e.g. `gpt-4`, `fast-summary`) |
| `prompt_regex` | Pattern match on the user message (advisory; see note below) |
| `max_tokens_gt` | Deny requests asking for more than N tokens |

Actions include `deny`, `require_approval`, and `redirect_to_alias` (replace the requested alias with an alternative before dispatch).

A denied request never contacts the provider. The API key is never read from the vault. An audit event records the decision with the matched rule, and the response body carries the typed error `policy_denied_llm`.

::: warning Prompt-content policy
Regex rules on prompt content are advisory: they match only the literal request body and are easily circumvented by rephrasing. For robust enforcement, use `deny` rules on `provider_driver` or `model_alias` to restrict which providers and capabilities a tenant may access. Prompt-content rules are useful for audit-logging patterns of interest, not as a security boundary.
:::

## Console

The operator Console includes dedicated screens for the LLM gateway:

- **`/llm/providers`** — provider CRUD, split into Built-in and Custom tabs. The Custom tab offers a one-click template catalog.
- **`/llm/models`** — model alias CRUD with capability checkboxes and default-parameter editing.
- **`/llm/quotas`** — per-tenant quota form with a live usage indicator.
- **`/llm/costs`** — daily and per-model cost dashboards.
- **`/llm/health`** — live per-provider, per-key health status drawn from the engine's `Health()` call.
- **`/llm/playground`** — interactive chat and completion tester with a side panel showing span, audit, cost, and token counts. Tool selection pulls from the live MCP catalog so tool-use bridging is testable directly from the Console.
- **`/llm/sessions`** — chat session history with transcript replay.

See [Console](/concepts/console) for the shared navigation and authentication model.

## Error codes

| Code | HTTP status | Meaning |
|------|-------------|---------|
| `quota_exceeded` | 429 | Request would breach a per-tenant limit |
| `provider_unavailable` | 503 | Upstream provider is unreachable; all keys failed |
| `model_unknown` | 400 | Alias does not exist in the tenant's registry |
| `unsupported_capability` | 400 | Model alias does not declare the requested capability |
| `policy_denied_llm` | 403 | A policy rule denied the request |
| `custom_provider_invalid` | 400 | The `custom_openai` provider config is malformed |
| `engine_unavailable` | 503 | The embedded engine has not been initialised |

## Related

- [LLM providers](/concepts/llm-providers) — built-in drivers, custom OpenAI-compatible providers, and the template catalog.
- [LLM routing](/concepts/llm-routing) — weighted key selection, automatic fallback, and per-alias routing rules.
- [Semantic cache](/concepts/semantic-cache) — exact and semantic matching, TTL configuration, and cache invalidation.
- [Virtual Keys](/concepts/virtual-keys) — per-consumer credential scoping layered on top of tenant-level keys.
- [Hierarchical budgets](/concepts/hierarchical-budgets) — budget trees from tenant down to individual Virtual Keys.
- [Credentials vault](/concepts/credentials-vault) — how provider API keys are stored and resolved at dispatch time.
- [Policy](/concepts/policy) — rule matchers and actions that govern both MCP tool calls and LLM requests.
- [Observability](/concepts/observability) — spans, metrics, and audit events produced by every LLM call.
