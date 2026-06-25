# Providers & model catalog

Portico's LLM gateway routes inference traffic to whichever upstream providers an operator has registered, then exposes those providers to clients under a stable set of **model aliases** that the operator controls. This page covers the full model catalog system: native provider drivers, the `custom_openai` escape hatch for OpenAI-compatible endpoints, per-tenant isolation, per-provider credential vaulting, weighted multi-key routing, model alias resolution, and the quota layer that caps each tenant's usage.

If you are new to the LLM gateway, start with [LLM Gateway](/concepts/llm-gateway) for the end-to-end picture, then return here for provider and model configuration depth.

---

## How the catalog fits into the gateway

Every chat completion, embedding, or moderation request that arrives at `/v1/*` passes through four layers before touching a provider:

1. **JWT validation + tenant extraction** — the same validator the MCP gateway uses. New scopes: `llm:invoke`, `llm:embed`, `llm:moderate`.
2. **Model alias resolution** — the `model` field in the request body is an alias the operator defined. The resolver looks up the alias in `tenant_llm_models` and returns the concrete `(provider_name, provider_model)` pair for that tenant.
3. **Policy evaluation** — optional rules that can deny, redirect to another alias, or require approval based on provider, driver, alias, prompt content, or token ceiling.
4. **Quota pre-check** — per-tenant rolling-window counters (requests per minute, tokens per minute, tokens per day) are verified before the provider call is dispatched.

The engine itself — a pure-Go, Apache-2.0 LLM engine embedded in the binary — receives a fully-resolved request. It reads provider configuration and credentials from Portico's per-tenant registry and vault on every dispatch. No provider key is cached in memory past a single request's lifetime.

---

## Native provider drivers

The embedded LLM engine ships native support for a set of well-known inference providers. Portico exposes these through its own provider registry, meaning every native driver is available to any tenant once the operator creates a provider row for it.

The driver names recognized natively are (as registered by the engine driver in `internal/llm/engine/`):

| Driver name | Category |
|---|---|
| `openai` | Chat, embeddings, moderation |
| `azure` | Chat, embeddings (Azure-hosted endpoint) |
| `anthropic` | Chat |
| `bedrock` | Chat, embeddings (AWS-hosted) |
| `gemini` | Chat |
| `vertex` | Chat, embeddings (GCP-hosted) |
| `mistral` | Chat, embeddings |
| `groq` | Chat |
| `cohere` | Chat, embeddings, rerank |
| `ollama` | Chat, embeddings (self-hosted) |
| `openrouter` | Chat (meta-router) |
| `huggingface` | Chat, embeddings |
| `cerebras` | Chat |
| `perplexity` | Chat |
| `parasail` | Chat |
| `nebius` | Chat |
| `elevenlabs` | Audio |
| `sgl` | Chat (SGLang) |
| `xai` | Chat |

Each driver name maps to a concrete code path inside the engine that understands that provider's wire protocol, error shapes, and authentication conventions. The driver name is what you supply in the `driver` field when creating a provider.

::: info Custom and regional endpoints
Not every OpenAI-compatible deployment has its own native driver. Operators covering regional endpoints, internal inference clusters, or providers not in the list above use the `custom_openai` driver described in the next section.
:::

---

## The `custom_openai` driver

Any endpoint that speaks the OpenAI chat completions API can be registered as a `custom_openai` provider. The engine routes these through the same OpenAI-compatible code path as the native `openai` driver, but with the `base_url`, `chat_path`, and HTTP headers you supply.

This driver covers:

- Self-hosted [vLLM](https://github.com/vllm-project/vllm) or similar inference servers
- Regional or enterprise mirror endpoints
- Mock servers for integration testing (an `httpbun`-shaped server works end-to-end)
- Any provider whose OpenAI compatibility layer is not yet a native driver in the engine

### One-click templates

Portico ships a **template catalog** (`GET /api/llm/providers/templates`) with curated presets that pre-fill `base_url`, `chat_path`, and the default header set for common deployments. Creating a provider from a template requires only the credential reference:

```bash
# List available templates
portico llm providers templates

# Create a provider from a template
portico llm providers from-template --template deepseek
```

```http
POST /api/llm/providers/from-template
Content-Type: application/json
Authorization: Bearer <token>

{
  "template": "deepseek",
  "name": "deepseek-prod",
  "credential_ref": "deepseek-api-key"
}
```

Template slugs available out of the box: `deepseek`, `together`, `anyscale`, `lepton`, `lambda`, `vllm-internal`, `ollama-internal`, `httpbun-mock`, and an `other` stub for fully custom endpoints.

---

## Per-tenant isolation

Every provider and model alias is scoped to a tenant. The underlying tables enforce this at the database level:

```sql
-- from migration 0014
CREATE TABLE IF NOT EXISTS tenant_llm_providers (
    tenant_id      TEXT NOT NULL,
    name           TEXT NOT NULL,
    driver         TEXT NOT NULL,
    config_json    TEXT NOT NULL DEFAULT '{}',
    credential_ref TEXT,
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, name)
);
```

The primary key is `(tenant_id, name)`, so two tenants can each have a provider named `openai-main` that points at different accounts. No query on these tables is valid without a `WHERE tenant_id = ?` clause — the storage interface (`internal/storage/ifaces/LLMProviderStore`) takes `tenantID` as an explicit parameter on every method.

Tenant identity flows from the JWT through the request context and is extracted via `tenant.MustFrom(ctx)` before any provider or model lookup occurs.

---

## Registering a provider

Providers are created via the REST API (or the `portico llm providers` CLI subcommands). The `name` is a free-form identifier the operator chooses; `driver` must be one of the native driver names or `custom_openai`.

### Built-in provider example (OpenAI)

```http
POST /api/llm/providers
Content-Type: application/json
Authorization: Bearer <token>

{
  "name": "openai-main",
  "driver": "openai",
  "credential_ref": "openai-api-key",
  "enabled": true
}
```

```bash
portico llm providers put --name openai-main --driver openai \
  --credential-ref openai-api-key
```

The `credential_ref` names a vault entry for this tenant. The key is never stored in the provider row — only the reference name is. See [Credentials & Vault](/concepts/credentials-vault) for how vault entries are created.

### Azure-hosted provider example

Azure-hosted models require an endpoint URL and API version in `config_json`:

```http
POST /api/llm/providers
Content-Type: application/json
Authorization: Bearer <token>

{
  "name": "azure-eastus",
  "driver": "azure",
  "config_json": {
    "base_url": "https://my-deployment.openai.azure.com",
    "api_version": "2024-02-01"
  },
  "credential_ref": "azure-api-key",
  "enabled": true
}
```

### Custom OpenAI-compatible endpoint

```http
POST /api/llm/providers
Content-Type: application/json
Authorization: Bearer <token>

{
  "name": "internal-vllm",
  "driver": "custom_openai",
  "config_json": {
    "base_url": "http://vllm.internal:8000",
    "headers": {
      "X-Cluster-ID": "prod-gpu-1"
    }
  },
  "credential_ref": "vllm-api-key",
  "enabled": true
}
```

The `base_url` and `headers` in `config_json` are read by the engine's `GetConfigForProvider` callback on every request dispatch. This means changing a provider's `base_url` takes effect immediately without a process restart.

---

## Credential vaulting and multi-key routing

### Default credential

The `credential_ref` on the provider row is its default key. The vault entry must exist for the tenant before the provider can be used.

### Weighted multi-key routing

A provider can hold multiple API keys with independent weights. The engine uses these weights to distribute traffic (and to fail over automatically when a key becomes unhealthy):

```http
POST /api/llm/providers/openai-main/keys
Content-Type: application/json
Authorization: Bearer <token>

{
  "credential_ref": "openai-key-primary",
  "weight": 1.0,
  "model_allowlist": [],
  "enabled": true
}
```

```http
POST /api/llm/providers/openai-main/keys
Content-Type: application/json
Authorization: Bearer <token>

{
  "credential_ref": "openai-key-secondary",
  "weight": 0.5,
  "model_allowlist": ["gpt-4o", "gpt-4o-mini"],
  "enabled": true
}
```

The `weight` values are relative: `1.0` and `0.5` means the primary key receives roughly twice the traffic of the secondary. The `model_allowlist` is a JSON array of upstream model names — an empty array means "all models." An empty `model_allowlist` on a key matches any model; the engine's matching rule is `len(allowlist) == 0 || contains(allowlist, model)`.

The underlying table is `tenant_llm_provider_keys` (migration 0014), with a `FOREIGN KEY … ON DELETE CASCADE` to the provider row.

### Key lifecycle

```bash
portico llm providers keys list   --provider openai-main
portico llm providers keys add    --provider openai-main \
  --credential-ref openai-key-dr  --weight 1.0
portico llm providers keys remove --provider openai-main --key-id <ULID>
```

When a key is deleted or disabled, the engine immediately stops sending to it. If only one enabled key remains, it handles all traffic.

::: warning Vault requirement
A `credential_ref` that does not exist in the vault causes that key to be silently skipped at dispatch time (not a startup error). Verify vault entries exist with `portico vault get <ref>` before registering a provider key.
:::

---

## Model aliases

Model aliases are per-tenant names that map to a `(provider_name, provider_model)` pair. Clients always refer to aliases, never to provider-internal model strings.

The underlying type (from `internal/storage/ifaces/llm_models.go`):

```go
type LLMModel struct {
    TenantID          string
    Alias             string      // e.g. "gpt-4", "fast-summary"
    ProviderName      string      // references tenant_llm_providers(name)
    ProviderModel     string      // e.g. "gpt-4o", "deepseek-chat"
    DefaultParamsJSON string      // JSON: temperature, top_p, max_tokens
    Capabilities      string      // JSON array: "chat"|"completion"|"embedding"|"moderation"|"tool_use"
    Enabled           bool
}
```

### Defining an alias

```http
POST /api/llm/models
Content-Type: application/json
Authorization: Bearer <token>

{
  "alias": "gpt-4",
  "provider_name": "openai-main",
  "provider_model": "gpt-4o",
  "default_params": {
    "temperature": 0.7,
    "max_tokens": 4096
  },
  "capabilities": ["chat", "tool_use"],
  "enabled": true
}
```

```bash
portico llm models put \
  --alias gpt-4 \
  --provider openai-main \
  --model gpt-4o \
  --capabilities chat,tool_use
```

### Per-tenant alias divergence

Tenant A and Tenant B can bind the same alias name to different providers and models:

| Tenant | Alias | Provider | Upstream model |
|---|---|---|---|
| `tenant-a` | `gpt-4` | `azure-eastus` | `gpt-4-deployment-1` |
| `tenant-b` | `gpt-4` | `openai-main` | `gpt-4o` |
| `tenant-b` | `fast-summary` | `internal-vllm` | `llama-3.1-8b-instruct` |

A client from Tenant A sending `"model": "gpt-4"` reaches the Azure-hosted deployment. The same alias string from Tenant B reaches a different provider entirely. The resolver queries `WHERE tenant_id = ? AND alias = ?` — cross-tenant leakage is structurally impossible.

### Alias as the clients' API surface

`GET /v1/models` returns the list of enabled aliases for the requesting tenant (not the provider's model catalog). This means the alias layer is exactly what external clients see; operators can rename, retire, or re-point aliases without requiring clients to update their configuration.

---

## Per-tenant quotas

Each tenant has a set of rate and usage limits. Defaults from migration `0016_llm_quotas.sql`:

| Limit | Default |
|---|---|
| `requests_per_minute` | 600 |
| `tokens_per_minute` | 200,000 |
| `tokens_per_day` | 4,000,000 |
| `cost_usd_per_day` | 100.00 |

Requests that would exceed any limit receive `429 quota_exceeded` before the provider call is made. An audit event records every exceedance.

```http
PUT /api/llm/quotas
Content-Type: application/json
Authorization: Bearer <token>

{
  "requests_per_minute": 1200,
  "tokens_per_minute": 500000,
  "tokens_per_day": 10000000,
  "cost_usd_per_day": 250.00
}
```

```bash
portico llm quotas put \
  --requests-per-minute 1200 \
  --tokens-per-minute 500000
```

The quota enforcer (`internal/llm/quota`) maintains in-memory rolling windows per tenant. The pre-call `Check` and post-call `RecordUsage` are separate operations so the window is updated even when the provider returns an error. Phase 19 (production scale-out) replaces the per-process windows with a shared counter for multi-instance deployments.

For hierarchical budgets at the Virtual Key, team, and customer level, see [Hierarchical Budgets](/concepts/hierarchical-budgets).

---

## Policy rules on providers and models

The policy engine can evaluate LLM calls against provider-specific matchers. A rule can deny a request, redirect it to a different alias, or queue it for approval:

```yaml
# Deny all direct calls to a model alias
- matchers:
    - type: model_alias
      value: gpt-4
  action: deny
  reason: "Use 'fast-summary' alias for this tenant"

# Require approval for calls that exceed 4000 tokens
- matchers:
    - type: max_tokens_gt
      value: 4000
  action: require_approval

# Redirect one alias to another (e.g. redirect deprecated alias)
- matchers:
    - type: model_alias
      value: gpt-3.5-turbo
  action: redirect_to_alias
  alias: fast-summary
```

Policy decisions are evaluated before the quota pre-check and the provider dispatch. A denied request never reaches the vault key fetch or the upstream network. Audit events carry the decision and the matched rule for every deny and redirect.

---

## Engine architecture and the driver seam

The LLM engine is not hardcoded into the gateway handler. It is accessed through the `Engine` interface defined in `internal/llm/engine/ifaces/`:

```go
type Engine interface {
    Name() string
    ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
    Embedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
    ProvidersSupported() []string
    Health(ctx context.Context) ([]ProviderHealth, error)
}
```

The current implementation wraps a pure-Go, Apache-2.0 LLM engine. It self-registers at `init()` time and is pulled into the binary by a single blank import in `cmd/portico/llm_wiring.go`. Nothing else in production code imports the concrete engine package — callers only ever see the `ifaces.Engine` interface.

This means swapping or extending the engine in a future phase is a single-driver change with no impact on the gateway handler, the alias resolver, or the quota enforcer.

`ProvidersSupported()` returns the driver names the engine can route to (`openai`, `azure`, … plus `custom_openai`). The provider registry validates this list at creation time: attempting to register a provider with a driver the engine does not support fails immediately with a descriptive error, rather than at the first dispatch.

---

## REST API reference

### Providers

```
GET    /api/llm/providers
POST   /api/llm/providers
GET    /api/llm/providers/{name}
PUT    /api/llm/providers/{name}
DELETE /api/llm/providers/{name}

GET    /api/llm/providers/templates
POST   /api/llm/providers/from-template

GET    /api/llm/providers/{name}/keys
POST   /api/llm/providers/{name}/keys
DELETE /api/llm/providers/{name}/keys/{key_id}

GET    /api/llm/health
```

### Models

```
GET    /api/llm/models
POST   /api/llm/models
GET    /api/llm/models/{alias}
PUT    /api/llm/models/{alias}
DELETE /api/llm/models/{alias}
```

### OpenAI-compatible surface (tenant-facing)

```
GET    /v1/models              # lists enabled aliases for the requesting tenant
GET    /v1/models/{alias}
POST   /v1/chat/completions
POST   /v1/completions
POST   /v1/embeddings
POST   /v1/moderations
```

### Quotas

```
GET    /api/llm/quotas
PUT    /api/llm/quotas
```

---

## Console

The `/llm/providers` console page has two tabs — **Built-in** and **Custom** — matching the two categories of providers. The Built-in tab presents a driver picker over the native driver names; driver-specific fields (Azure deployment name, Bedrock region, Vertex project ID) render conditionally. The Custom tab opens with the template catalog so operators pick a preset and supply only a credential reference.

The `/llm/models` page manages alias definitions. The provider model dropdown in the create form lazy-loads model names from the provider's upstream `/models` API when the provider itself supports listing them.

Health status for every registered key is visible at `/llm/health`, backed by `GET /api/llm/health`.

---

## Related

- [LLM Gateway](/concepts/llm-gateway) — northbound API, tool-use bridging, and the end-to-end request flow
- [LLM Routing & fallback](/concepts/llm-routing) — weighted key selection, fallback behavior, and per-call provider choice
- [Credentials & Vault](/concepts/credentials-vault) — how vault entries are created and referenced from provider rows
- [Hierarchical Budgets](/concepts/hierarchical-budgets) — Virtual Key, team, and customer budget layers on top of tenant quotas
- [Semantic Cache](/concepts/semantic-cache) — tenant-isolated response caching in front of the provider call
- [Policy](/concepts/policy) — full policy rule reference including LLM-specific matchers and actions
- [Agent Profiles](/concepts/agent-profiles) — per-consumer model alias allowlists and scope enforcement
