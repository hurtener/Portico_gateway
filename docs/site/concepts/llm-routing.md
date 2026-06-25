# Routing & fallback

Every request that arrives at Portico's LLM gateway carries a model name — the *alias* a client application was configured to use. Between that alias and the actual upstream provider call sit several layers of routing logic: alias resolution, key selection, weighted load balancing, automatic fallback, quota enforcement, and policy-based redirection. This page explains each layer, how they compose, and how to configure them.

## Overview

The routing stack executes in this order for each non-streaming or streaming completion request:

1. **Quota pre-check.** The per-tenant rolling-window counters are read atomically. If the request would exceed any limit (`requests_per_minute`, `tokens_per_minute`, `tokens_per_day`, or `cost_usd_per_day`) the call is rejected with `429 quota_exceeded` before any upstream contact is made.
2. **Policy evaluation.** LLM-specific policy rules are evaluated against the resolved alias, provider driver, and request parameters. A matching `deny` rule short-circuits here; a `redirect_to_alias` rule rewrites the alias before resolution continues.
3. **Alias resolution.** The alias is looked up in the per-tenant model registry. A single row maps alias → `(provider_name, provider_model)`, including any default generation parameters (temperature, max tokens) the operator pinned at registration time.
4. **Key selection.** Portico's LLM engine reads the enabled key roster for that provider from the vault. Each key carries a numeric weight and an optional model allowlist. The engine distributes traffic across eligible keys proportional to their weights.
5. **Upstream dispatch.** The chosen key is used to call the upstream provider. The actual secret is fetched from the vault on every dispatch; no plaintext credential is cached between requests.
6. **Fallback.** If the upstream call fails or the chosen key is reported unhealthy, the engine retries with the next eligible key in weight order. If all keys are exhausted, `503 provider_unavailable` is returned and the failure is recorded in the health view.
7. **Post-call accounting.** Token usage is recorded against the rolling-window counters. Cost (input tokens × unit price + output tokens × unit price) is accumulated into the per-tenant daily cost ledger.

## Model aliases

An alias is the stable name a client uses regardless of which provider or model version the operator has deployed behind it. Aliases are per-tenant: two tenants can register the same alias string pointing to completely different providers and models.

```json
// POST /api/llm/models
{
  "alias": "reasoning",
  "provider_name": "openai-prod",
  "provider_model": "o3-mini",
  "capabilities": ["chat", "tool_use"],
  "default_params": {
    "max_tokens": 8192
  }
}
```

The alias `reasoning` is what the client sends in the `model` field of its OpenAI-compatible request. The gateway resolves it to provider `openai-prod` and forwards `o3-mini` to that provider's endpoint. Another tenant in the same gateway instance could have `reasoning` point to a self-hosted model on a private endpoint.

`GET /v1/models` returns only the aliases that are enabled for the authenticated tenant — never another tenant's aliases, and never raw provider model strings unless an operator explicitly aliases them.

::: tip Per-tenant alias isolation
Two tenants using the same alias name never share resolution state. There is no "global" model registry. Every lookup filters `WHERE tenant_id = ?` against the alias table before returning a result.
:::

## Weighted keys and load balancing

A single provider entry can have multiple API keys associated with it. Keys are stored in the vault and referenced by a `credential_ref`; only the reference — never the secret — is kept in the provider-key table.

Each key row carries:

| Field | Type | Meaning |
|---|---|---|
| `credential_ref` | string | Vault entry that holds the secret |
| `weight` | float64 | Proportional share of traffic (default 1.0) |
| `model_allowlist` | string[] | Models this key may serve; empty array means all |
| `enabled` | bool | When false, key is excluded from selection |

```http
POST /api/llm/providers/openai-prod/keys
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "credential_ref": "openai-key-primary",
  "weight": 3.0,
  "model_allowlist": [],
  "enabled": true
}
```

```http
POST /api/llm/providers/openai-prod/keys
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "credential_ref": "openai-key-secondary",
  "weight": 1.0,
  "model_allowlist": [],
  "enabled": true
}
```

With the configuration above, the primary key handles 75% of requests (weight 3 out of 4 total) and the secondary key handles 25%. The engine evaluates weights each time it selects a key; it does not maintain affinity across requests.

The `model_allowlist` field partitions traffic by model. An operator running a key that has quota limits on a specific model can restrict it:

```json
{
  "credential_ref": "openai-key-limited",
  "weight": 1.0,
  "model_allowlist": ["gpt-4o-mini", "gpt-3.5-turbo"],
  "enabled": true
}
```

A key with a non-empty `model_allowlist` is only eligible to serve requests for models in that list. The primary key (empty allowlist) is eligible for all models and absorbs traffic for models the restricted key cannot serve.

Keys for the same provider that share a driver are aggregated by the engine's account layer: when Portico's LLM engine asks for keys for the `openai` driver, it receives all enabled, eligible keys across every `openai`-driver provider the tenant has registered, flattened into one weighted list.

## Automatic fallback

Fallback is transparent to the caller and requires no configuration beyond registering more than one key. When the engine selects a key and the upstream returns an error (connection failure, authentication error, rate-limit response), it marks that key as unhealthy for the duration of the request and retries with the next eligible key by weight.

Fallback traversal is exhaustive: the engine tries every eligible key before giving up. Only if all keys are exhausted does it return `503 provider_unavailable` to the caller.

Every fallback event produces an audit record with event type `llm.fallback_used`:

```json
{
  "event": "llm.fallback_used",
  "tenant_id": "acme",
  "alias": "fast-chat",
  "provider_name": "openai-prod",
  "failed_key_id": "01J...",
  "serving_key_id": "01K...",
  "upstream_status": 429,
  "request_id": "req_..."
}
```

The `/api/llm/health` endpoint surfaces the engine's current view of each key's status, derived from the same probe data the fallback logic uses. A key that has been failing consistently appears red in the Console health table and in the health API response.

::: warning Zero-downtime failover
Fallback is per-request, not persistent. If the primary key is repeatedly unhealthy, every request pays the cost of a failed attempt before falling back. For a key that is permanently revoked or decommissioned, disable it explicitly via `DELETE /api/llm/providers/{name}/keys/{key_id}` or set `enabled: false` via `PUT`.
:::

## Multi-region key patterns

A provider with endpoints in multiple regions or under multiple organizational accounts is handled by registering either:

- **Multiple keys on one provider** — works when the same driver name and endpoint serve all keys. Use `model_allowlist` to partition models across keys if different accounts have access to different model versions.
- **Multiple providers with the same driver** — works when endpoints differ (e.g. different `base_url` for US vs. EU deployments). Register `openai-us` and `openai-eu` as two providers, each with their own endpoint in `config.base_url` and their own keys. Create two model aliases (`fast-chat-us`, `fast-chat-eu`) or use a policy rule to redirect based on request metadata.

## Quota enforcement

Portico enforces four independent quota dimensions per tenant, checked pre-call with rolling windows:

| Field | Default | Unit |
|---|---|---|
| `requests_per_minute` | 600 | requests in a 60-second window |
| `tokens_per_minute` | 200,000 | total tokens in a 60-second window |
| `tokens_per_day` | 4,000,000 | total tokens in a 24-hour window |
| `cost_usd_per_day` | 100.00 | USD accumulated in the current calendar day |

Any dimension set to zero means no limit on that dimension. Quotas are independent: a tenant can be within its per-minute token limit but hit its daily cost cap.

```http
PUT /api/llm/quotas
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "requests_per_minute": 300,
  "tokens_per_minute": 100000,
  "tokens_per_day": 2000000,
  "cost_usd_per_day": 50.00
}
```

When a quota fires, the response body carries the slug of the breached dimension:

```json
{
  "error": "quota_exceeded",
  "message": "llm quota exceeded: tokens_per_minute",
  "details": {
    "limit": "tokens_per_minute"
  }
}
```

Token counts for the pre-call check are estimated from the request; the post-call reconciliation uses the provider-reported token counts from the response.

### Cost-aware routing

The `cost_usd_per_day` quota dimension ties routing to spending. The cost of a call is computed as:

```
cost = (prompt_tokens / 1000 × input_per_1k) + (completion_tokens / 1000 × output_per_1k)
```

Unit prices per `(provider_driver, provider_model)` come from the price book (`GET /api/llm/costs/units`). Operators can override any entry or add entries for custom providers:

```http
POST /api/llm/costs/units
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "provider_driver": "custom_openai",
  "provider_model": "deepseek-chat",
  "input_per_1k": 0.00014,
  "output_per_1k": 0.00028
}
```

Daily cost rollups are queryable per tenant:

```http
GET /api/llm/costs?day=2026-06-24
Authorization: Bearer <admin-jwt>
```

```json
{
  "costs": [
    {
      "alias": "reasoning",
      "requests": 1240,
      "input_tok": 3100000,
      "output_tok": 820000,
      "cost_usd": 47.82,
      "day": "2026-06-24"
    }
  ]
}
```

::: info Hierarchical budgets
The per-tenant `cost_usd_per_day` quota is the baseline cost gate. For organisations that need sub-tenant cost accountability — by team, customer, application, or environment — Virtual Keys carry their own independent budgets that are checked before the tenant-level quota. See [Hierarchical budgets](/concepts/hierarchical-budgets) and [Virtual keys](/concepts/virtual-keys).
:::

## Policy-based routing

The policy engine evaluates LLM-specific matchers before a request reaches the alias resolver. Relevant matchers include `provider`, `provider_driver`, `model_alias`, and `max_tokens_gt`. The `redirect_to_alias` action rewrites the model alias mid-flight, enabling controlled promotion or degradation paths without changing client configuration.

A typical use case is routing to a lighter, lower-cost alias when a cost threshold is approaching:

```yaml
# portico.yaml policy excerpt
policy:
  rules:
    - id: degrade-on-budget-pressure
      match:
        model_alias: "reasoning"
        budget_headroom_pct_lt: 20
      action: redirect_to_alias
      params:
        alias: "fast-chat"
```

Another common pattern is blocking access to high-cost aliases for specific scopes:

```yaml
    - id: block-reasoning-from-read-only
      match:
        model_alias: "reasoning"
        scope_missing: "llm:premium"
      action: deny
      message: "reasoning alias requires llm:premium scope"
```

A `deny` rule fires before the upstream is contacted; the provider key is never used and the event is recorded as `policy_denied_llm` in the audit log.

See [Policy](/concepts/policy) for the full rule syntax and evaluation model.

## Observability

The routing layer emits span attributes and audit events for every dispatch. Relevant span attributes:

| Attribute | Value |
|---|---|
| `llm.alias` | The alias as received (before any redirect) |
| `llm.provider` | The provider name after alias resolution |
| `llm.provider_driver` | The driver string (e.g. `openai`, `anthropic`, `custom_openai`) |
| `llm.model` | The upstream model string forwarded to the provider |
| `llm.prompt_tokens` | Token count from provider usage response |
| `llm.completion_tokens` | Token count from provider usage response |
| `llm.cost_usd` | Computed cost for this call |
| `llm.tool_calls` | Number of tool calls in the response |

Audit events:

| Event | When |
|---|---|
| `llm.invoked` | Successful non-streaming call |
| `llm.streamed` | Streaming call completed |
| `llm.fallback_used` | A key failed and another served the request |
| `llm.quota_exceeded` | A quota dimension was exceeded |
| `llm.failed` | All keys exhausted or engine error |
| `llm.tool_bridged` | Model-issued tool call dispatched via the MCP catalog |

## Complete configuration example

The following registers two providers (a built-in and a custom-compatible endpoint), two keys with weights, two model aliases, and a quota that includes a daily cost cap.

```http
### Step 1: register the primary provider
POST /api/llm/providers
Authorization: Bearer <admin-jwt>

{
  "name": "openai-prod",
  "driver": "openai",
  "credential_ref": "openai-primary",
  "enabled": true
}

### Step 2: add weighted keys
POST /api/llm/providers/openai-prod/keys

{ "credential_ref": "openai-primary",   "weight": 3.0, "enabled": true }

POST /api/llm/providers/openai-prod/keys

{ "credential_ref": "openai-secondary", "weight": 1.0, "enabled": true }

### Step 3: register a custom-compatible endpoint
POST /api/llm/providers

{
  "name": "internal-inference",
  "driver": "custom_openai",
  "config": {
    "base_url": "https://inference.internal.example.com/v1",
    "headers": { "X-Cluster-Id": "gpu-cluster-1" }
  },
  "credential_ref": "internal-api-key",
  "enabled": true
}

POST /api/llm/providers/internal-inference/keys

{ "credential_ref": "internal-api-key", "weight": 1.0, "enabled": true }

### Step 4: define model aliases
POST /api/llm/models

{
  "alias": "fast-chat",
  "provider_name": "openai-prod",
  "provider_model": "gpt-4o-mini",
  "capabilities": ["chat", "tool_use"],
  "default_params": { "max_tokens": 4096 }
}

POST /api/llm/models

{
  "alias": "bulk-summarise",
  "provider_name": "internal-inference",
  "provider_model": "llama-3-8b-instruct",
  "capabilities": ["chat"],
  "default_params": { "temperature": 0.2, "max_tokens": 2048 }
}

### Step 5: set quota including a daily cost cap
PUT /api/llm/quotas

{
  "requests_per_minute": 500,
  "tokens_per_minute": 150000,
  "tokens_per_day": 3000000,
  "cost_usd_per_day": 75.00
}
```

A client pointed at `http://portico.example.com/v1` with a valid bearer token can now call:

```bash
curl -s https://portico.example.com/v1/chat/completions \
  -H "Authorization: Bearer $PORTICO_TOKEN" \
  -d '{"model":"fast-chat","messages":[{"role":"user","content":"Summarise this document."}]}'
```

The gateway resolves `fast-chat` to `openai-prod` / `gpt-4o-mini`, selects among the two registered keys proportional to weights 3:1, checks quota, dispatches, records cost, and returns a standard OpenAI-shaped response. If the primary key is unavailable, the secondary key serves without client-visible error.

## CLI reference

```bash
# List model aliases
portico llm models list

# Add a model alias
portico llm models put \
  --alias reasoning \
  --provider openai-prod \
  --model o3-mini \
  --capabilities chat,tool_use

# List provider keys
portico llm providers keys list --provider openai-prod

# Add a weighted key
portico llm providers keys add \
  --provider openai-prod \
  --credential-ref openai-secondary \
  --weight 1.0

# View quota
portico llm quotas get

# View today's cost breakdown by alias
portico llm costs --day "$(date +%Y-%m-%d)"

# Check per-key health
portico llm health
```

## Related

- [LLM gateway](/concepts/llm-gateway) — architecture overview of the OpenAI-compatible northbound surface
- [LLM providers](/concepts/llm-providers) — registering built-in and custom-compatible provider backends
- [Hierarchical budgets](/concepts/hierarchical-budgets) — per-Virtual-Key, team, and customer budget enforcement layered on top of per-tenant quotas
- [Virtual keys](/concepts/virtual-keys) — issuing scoped, budget-bound credentials for sub-tenant applications
- [Policy](/concepts/policy) — rule syntax for model-level deny, require-approval, and redirect actions
- [Semantic cache](/concepts/semantic-cache) — exact and similarity-based caching to reduce upstream dispatch volume and cost
- [Audit](/concepts/audit) — querying `llm.fallback_used`, `llm.quota_exceeded`, and cost events
