# Manage providers, keys & budgets

This guide walks an operator through the complete setup cycle for Portico's LLM gateway: registering a provider, storing its API key in the vault, creating a model alias, minting a Virtual Key for an application, attaching hierarchical budgets, and making a governed OpenAI-compatible call. All steps are shown as REST calls and equivalent CLI commands.

::: info Prerequisites
The LLM gateway (Phase 13) and governance primitives (Phase 15.5) must be enabled in your `portico.yaml`. Run `portico dev` to start a local server; in dev mode the JWT requirement is bypassed and the default tenant is `dev`.
:::

## Overview

Portico's LLM gateway sits on top of a pure-Go, Apache-2.0 LLM engine that natively routes to more than twenty provider backends. The operator control surface has four layers:

| Layer | What it governs |
|---|---|
| **Provider** | Which upstream backend to call and with which credentials |
| **Model alias** | The public name a client uses — decoupled from the provider's internal model identifier |
| **Virtual Key** | A scoped, per-application `pk-portico-*` token with its own scope set and allowlists |
| **Budget** | Token, request, and cost limits attached to a Virtual Key, Team, or Customer |

Every layer is per-tenant. Cross-tenant access is impossible by construction.

## Step 1 — Store the provider key in the vault

Provider API keys live entirely inside Portico's credential vault. The LLM engine reads them at dispatch time; they never appear in logs, error responses, or audit events.

```bash
# Write the key. --value can also be read from stdin for shell-history hygiene.
portico vault put \
  --tenant acme \
  --name llm/openai/primary \
  --value sk-...
```

The `--name` argument is a free-form string. Using a path-style convention (`llm/<provider>/<purpose>`) makes the roster easier to scan:

```bash
portico vault list --tenant acme
```

You reference this vault entry by its name (`llm/openai/primary`) when you register the provider in Step 2. The gateway never substitutes the plaintext value anywhere except the single provider call inside the engine.

## Step 2 — Register a provider

A **provider** binds a driver name (one of the LLM engine's supported backends) to a vault entry and optional driver-specific configuration.

```http
POST /api/llm/providers
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "name": "openai-prod",
  "driver": "openai",
  "credential_ref": "llm/openai/primary",
  "enabled": true
}
```

The `driver` field selects the backend. Supported built-in values include `openai`, `anthropic`, `azure`, `bedrock`, `gemini`, `groq`, `mistral`, `ollama`, `vllm`, and others the engine covers natively. For any OpenAI-compatible endpoint that is not one of the built-in names, use `driver: custom_openai` and supply a `base_url` in `config`:

```http
POST /api/llm/providers
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "name": "internal-vllm",
  "driver": "custom_openai",
  "config": {
    "base_url": "http://gpu-cluster.internal/v1"
  },
  "credential_ref": "llm/internal-vllm/key",
  "enabled": true
}
```

All writes require the `admin` scope. Reads (`GET /api/llm/providers`, `GET /api/llm/providers/{name}`) are scoped to the calling tenant and require authentication.

### Adding a second key (weighted routing and fallback)

A single provider can have multiple credential entries. The LLM engine routes across them by weight and falls back automatically when one becomes unavailable. A `llm.fallback_used` audit event records every fallback.

```http
POST /api/llm/providers/openai-prod/keys
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "credential_ref": "llm/openai/secondary",
  "weight": 0.3,
  "enabled": true
}
```

`weight` is a relative floating-point value. A primary key with weight `1.0` and a secondary with `0.3` means roughly 77% of traffic goes to the primary. An empty `model_allowlist` array means the key handles all models; populate the array to restrict a key to a specific model subset.

To remove a key: `DELETE /api/llm/providers/{name}/keys/{key_id}`.

## Step 3 — Register a model alias

A **model alias** is the public name clients use in `"model"` fields. It maps to a `(provider_name, provider_model)` pair and can include default parameters.

```http
POST /api/llm/models
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "alias": "gpt-4",
  "provider_name": "openai-prod",
  "provider_model": "gpt-4o",
  "capabilities": ["chat", "tool_use"],
  "default_params": { "temperature": 0.7, "max_tokens": 4096 },
  "enabled": true
}
```

Different tenants can define the same alias pointing at different providers. Tenant A's `gpt-4` may resolve to a European Azure deployment; Tenant B's to OpenAI directly. No cross-tenant alias leakage is possible.

The `GET /v1/models` endpoint (OpenAI-compatible) returns the calling tenant's enabled aliases, so existing clients using the standard models list endpoint get accurate results.

## Step 4 — Set tenant-level quotas

Before issuing Virtual Keys, set the tenant-level quota baseline. Virtual Key and team-level budgets (Step 5) are additional layers on top of this.

```http
PUT /api/llm/quota
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "requests_per_minute": 600,
  "tokens_per_minute": 200000,
  "tokens_per_day": 4000000,
  "cost_usd_per_day": 100.00
}
```

Read the current quota: `GET /api/llm/quota`. A zero in any field means that dimension is unlimited. Quota violations return `429` with a typed error body containing the `quota_exceeded` slug.

## Step 5 — Mint a Virtual Key for an application

A **Virtual Key** is a `pk-portico-*` token scoped to a single application or environment. It authenticates the same bearer slot as a JWT but resolves to narrower permissions. The secret is generated once, returned in the creation response, and never retrievable again — only a `salt + HMAC-SHA256(salt, secret)` pair is stored.

```http
POST /api/governance/virtual-keys
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "name": "analytics-service-prod",
  "scopes": ["llm:invoke"],
  "provider_allowlist": ["openai"],
  "model_allowlist": ["gpt-4"],
  "parent_kind": "none"
}
```

The response carries the token **once**:

```json
{
  "virtual_key": {
    "id": "vk_a1b2c3d4e5f6...",
    "name": "analytics-service-prod",
    "scopes": ["llm:invoke"],
    "provider_allowlist": ["openai"],
    "model_allowlist": ["gpt-4"],
    "enabled": true,
    "created_at": "2026-06-25T08:00:00Z"
  },
  "token": "pk-portico-vk_a1b2c3d4e5f6....AbCdEfGh..."
}
```

Copy `token` to the application's secret store immediately. Any subsequent `GET /api/governance/virtual-keys/{id}` call returns the VK metadata but never the secret.

### Attaching a Virtual Key to a governance group

VKs can optionally belong to a Team or Customer for hierarchical budget roll-up:

```bash
# Create a customer first (or use an existing one)
portico governance customers create \
  --tenant acme \
  --name "Analytics Division"

# Create a team within that customer
portico governance teams create \
  --tenant acme \
  --name "data-platform" \
  --customer-id <customer-id>
```

Then set `"parent_kind": "team"` and `"parent_id": "<team-id>"` in the VK create request. Budgets placed on the Team and Customer levels will aggregate across all VKs they contain.

### Rotating and revoking

```http
# Rotate — old secret becomes invalid immediately; new secret returned once.
POST /api/governance/virtual-keys/{id}/rotate
Authorization: Bearer <admin-jwt>

# Revoke permanently.
DELETE /api/governance/virtual-keys/{id}
Authorization: Bearer <admin-jwt>
```

Revocation takes effect within approximately one second across running instances (the in-memory resolver cache entry is invalidated synchronously on the instance handling the request; other instances drain their TTL).

## Step 6 — Attach hierarchical budgets

Budgets are independent limits on requests, tokens, or cost, attached to any scope level. The enforcer checks from most-specific (Virtual Key) to least-specific (Tenant) before each call. The first level that would be exceeded fires a `429` with `details.level` indicating which limit tripped.

```http
# VK-level cost budget: $5 per day, calendar-aligned.
POST /api/governance/budgets
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "scope_kind": "vk",
  "scope_id": "vk_a1b2c3d4e5f6...",
  "metric": "cost_usd",
  "period": "1d",
  "alignment": "calendar",
  "limit_val": 5.00,
  "enabled": true
}
```

```http
# Team-level token budget: 1,000,000 tokens per hour, rolling window.
POST /api/governance/budgets
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "scope_kind": "team",
  "scope_id": "<team-id>",
  "metric": "tokens",
  "period": "1h",
  "alignment": "rolling",
  "limit_val": 1000000,
  "enabled": true
}
```

Valid `metric` values: `requests`, `tokens`, `cost_usd`. Valid `period` values: `1m`, `1h`, `1d`, `1w`, `1M`, `1Y`. Post-call reconciliation updates all applicable levels atomically in a single transaction.

### Checking headroom

```http
GET /api/governance/virtual-keys/{id}/budget
Authorization: Bearer <admin-jwt>
```

Returns a `levels` array with `used`, `limit`, `headroom_pct`, and `resets_at` for each applicable level (VK, Team, Customer, Tenant). The Console displays this as stacked headroom bars.

At 80% of any budget, a `llm.budget_warning` audit event fires once per window. At 95%, `llm.budget_critical` fires plus any configured webhook on the Customer record.

## Step 7 — Make a governed call

With the provider, model alias, and Virtual Key in place, any OpenAI-compatible client can call Portico directly. The bearer token is the `pk-portico-*` key issued in Step 5.

```bash
curl -s https://gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer $PORTICO_VK" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "user", "content": "Summarise this week'\''s anomaly report."}
    ]
  }'
```

The gateway enforces the full governing envelope before the upstream call reaches the provider:

1. **Auth** — bearer string is matched to the VK by prefix (`pk-portico-`), HMAC-verified, and resolved to tenant + scope set + allowlists.
2. **Agent Profile** — if the VK is bound to a profile, the profile's allowed models and servers are intersected with the VK's own allowlists (most-restrictive wins).
3. **Budget pre-check** — VK → Team → Customer → Tenant budgets are checked in order; the first violation fires `429`.
4. **Policy** — any policy rules scoped to `model_alias`, `provider_driver`, or prompt content are evaluated.
5. **Quota** — per-tenant per-minute and per-day rate/token counters are checked.
6. **Dispatch** — the alias resolves to `(provider, model)`; the engine fetches the provider key from the vault at dispatch time and calls the upstream.
7. **Post-call** — cost is computed and all applicable budget ledger entries are updated atomically; audit events (`llm.invoked`, `llm.streamed`) are emitted after redaction.

### Streaming

Pass `"stream": true` in the request body to receive Server-Sent Events in OpenAI's `data: {...}` format. The gateway sends a heartbeat comment every 15 seconds on long-running generations to prevent proxy timeouts.

### Inspecting cost and usage

```http
GET /api/llm/costs?day=2026-06-25
Authorization: Bearer <admin-jwt>
```

Returns a per-alias cost rollup for the day. For a time series: `GET /api/llm/costs?from=2026-06-01&to=2026-06-25`. Cost is computed from a per-provider unit price table (`GET /api/llm/costs/prices`); operators can override prices for custom or on-premises providers with `PUT /api/llm/costs/prices`.

### Provider live health

```http
GET /api/llm/health
Authorization: Bearer <admin-jwt>
```

Returns the engine's live view of each configured `(provider, key)` pair: last call timestamp, success rate over the last five minutes, and last error. Rows that the engine reports as failing render as red in the Console. When a key fails and a fallback key exists, the engine selects the fallback automatically and records a `llm.fallback_used` audit event.

## Semantic cache

When the semantic cache is enabled, identical (or semantically similar) requests are served from cache with no upstream call. The response includes `cache_hit: true` in the extended metadata. Configure the cache in `portico.yaml` or via `PUT /api/llm/cache/config` (after startup, driver changes require a restart).

To flush cache entries for a specific alias after a provider model change:

```http
POST /api/llm/cache/invalidate
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{ "alias": "gpt-4" }
```

Invalidation by VK scope: supply `"scope_id": "vk_a1b2c3d4e5f6..."` instead. Pass `"all": true` to flush the entire tenant namespace.

Clients may opt out of caching per request with standard HTTP headers:

- `Cache-Control: no-store` — neither read from nor write to the cache.
- `Cache-Control: no-cache` — read from upstream but write the result to cache.

See [Semantic Cache](/concepts/semantic-cache) for driver configuration and similarity thresholds.

## Governance CLI reference

The `portico governance` subcommand operates offline against the data directory (same pattern as `portico vault`):

```bash
# Customers
portico governance customers list   --tenant acme
portico governance customers create --tenant acme --name "Engineering" [--description "..."] [--webhook-url "..."]
portico governance customers get    --tenant acme --id <id>
portico governance customers update --tenant acme --id <id> --name "Engineering Platform"
portico governance customers delete --tenant acme --id <id>

# Teams
portico governance teams list   --tenant acme
portico governance teams create --tenant acme --name "backend" [--customer-id <id>]
portico governance teams get    --tenant acme --id <id>
portico governance teams update --tenant acme --id <id> --name "platform"
portico governance teams delete --tenant acme --id <id>

# Vault (provider keys)
portico vault put    --tenant acme --name llm/openai/primary
portico vault list   --tenant acme
portico vault delete --tenant acme --name llm/openai/primary
portico vault rotate-key --new-key <base64-32-bytes>
```

Virtual Key CRUD and budget CRUD are REST-only; there is no offline CLI for them because they involve secret generation (`Create`) or live ledger writes (`Budget`) that require the server to be running.

## Error reference

| HTTP status | Error slug | Cause |
|---|---|---|
| `403` | `vk_scope_violation` | VK's `provider_allowlist` or `model_allowlist` excludes this call |
| `403` | `model_disabled` | The requested alias exists but `enabled: false` |
| `404` | `model_not_found` | Alias not registered for this tenant |
| `404` | `not_found` | Provider or key not found |
| `429` | `quota_exceeded` | Tenant-level per-minute or per-day quota hit |
| `429` | `budget_exceeded` | VK/Team/Customer/Tenant budget exhausted; `details.level` identifies the level |
| `401` | `vk_revoked` | VK has been revoked |
| `503` | `llm_not_configured` | LLM gateway dependencies not wired in this build |

## Related

- [LLM Gateway](/concepts/llm-gateway) — architecture and request lifecycle
- [LLM Providers](/concepts/llm-providers) — driver reference and custom-provider templates
- [Virtual Keys](/concepts/virtual-keys) — HMAC binding, rotation, and MCP path
- [Hierarchical Budgets](/concepts/hierarchical-budgets) — VK → Team → Customer → Tenant model
- [Semantic Cache](/concepts/semantic-cache) — driver configuration and cache-key scoping
- [Credentials Vault](/concepts/credentials-vault) — vault storage model and key rotation
- [Agent Profiles](/concepts/agent-profiles) — binding a VK to a profile for additional allowlist enforcement
