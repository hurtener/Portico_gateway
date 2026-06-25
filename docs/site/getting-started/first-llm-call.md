# Your first LLM call

Portico's LLM gateway exposes an OpenAI-compatible HTTP API at the `/v1/` prefix. Any
client or library that speaks the OpenAI chat-completions wire format works unchanged —
you only swap the base URL to point at Portico and swap the API key for a
[Virtual Key](/concepts/virtual-keys). Provider credentials (the actual upstream API
keys) never leave the gateway; they live in Portico's encrypted vault.

This page walks through the four setup steps — store a provider key, register a
provider, create a model alias, issue a Virtual Key — then shows the actual call.

## How it works

```
Client                       Portico                        Upstream provider
  │                             │                                  │
  │  POST /v1/chat/completions  │                                  │
  │  Authorization: Bearer <VK> │                                  │
  │─────────────────────────────▶                                  │
  │                             │  1. Validate VK / JWT            │
  │                             │  2. Resolve model alias          │
  │                             │     → provider + upstream model  │
  │                             │  3. Evaluate policy + quota      │
  │                             │  4. Check semantic cache         │
  │                             │  5. Fetch provider key           │
  │                             │     from vault (per-request)     │
  │                             │─────────────────────────────────▶
  │                             │  6. Receive response             │
  │                             │◀─────────────────────────────────
  │                             │  7. Record cost + audit          │
  │◀─────────────────────────────                                  │
  │  OpenAI-shaped response     │                                  │
```

A few design points that differ from a direct provider call:

- **Model aliases are tenant-scoped.** The string you send in `"model"` is an alias
  configured by the operator, not a raw provider model id. Tenant A's `"fast"` can
  resolve to a different provider and model than Tenant B's `"fast"`.
- **Provider keys live in the vault.** Portico's LLM engine fetches the upstream key
  from the encrypted vault on every dispatch. No key is cached in memory across
  requests, and no key ever appears in logs, audit events, or error responses.
- **Virtual Keys are what applications present.** A Virtual Key carries the
  `llm:invoke` scope, optional provider and model allowlists, and links to an
  Agent Profile. It is the credential your application provides as a Bearer token;
  the operator's upstream API keys remain internal to the gateway.
- **The engine is a pure-Go, Apache-2.0 library embedded in the binary.** There is no
  sidecar process; the LLM routing layer shares the same listener, JWT validator, audit
  machinery, and span store as the MCP gateway.

## Prerequisites

- Portico is running (see [Installation](/getting-started/installation) and
  [Dev mode](/getting-started/dev-mode)).
- You have an upstream provider API key for at least one supported provider.
- You have the `PORTICO_VAULT_KEY` environment variable set (a base64-encoded 32-byte
  key). In dev mode this is auto-generated; in production you supply it.
- You have a JWT or Virtual Key with the `admin` scope for the setup steps, and one
  with the `llm:invoke` scope for the actual call. In dev mode the gateway accepts
  requests without a token.

## Step 1 — Store the provider key in the vault

Provider API keys are stored with `portico vault put`. The `--name` value becomes the
`credential_ref` you reference when you register the provider.

```bash
# Store an upstream API key for your provider.
# The secret is read from stdin so it never appears in shell history.
portico vault put \
  --tenant acme \
  --name openai-main \
  --from-stdin \
  --config portico.yaml
```

::: tip Vault key
`PORTICO_VAULT_KEY` must be set before running vault commands. The vault file path
defaults to `./vault.yaml`; override with `--path`.
:::

You can verify the entry exists (without revealing the secret) with:

```bash
portico vault list --tenant acme
```

## Step 2 — Register a provider

A provider record tells Portico which upstream service driver to use and which vault
entry holds the credential. Registration goes through the admin REST API.

The `driver` field must be one of the names supported by Portico's LLM engine. Built-in
drivers include: `openai`, `anthropic`, `azure`, `bedrock`, `cerebras`, `cohere`,
`elevenlabs`, `fireworks`, `gemini`, `groq`, `huggingface`, `mistral`, `nebius`,
`ollama`, `openrouter`, `parasail`, `perplexity`, `replicate`, `runway`, `sgl`,
`vertex`, `vllm`, and `xai`. For any other OpenAI-compatible endpoint — a self-hosted
instance, a regional mirror, a local mock — use `custom_openai` and set `base_url` in
the `config` block.

```bash
# Register a built-in provider. Requires admin scope.
curl -s -X POST http://localhost:8080/api/llm/providers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":           "openai-prod",
    "driver":         "openai",
    "credential_ref": "openai-main",
    "enabled":        true
  }'
```

Example response:

```json
{
  "name":           "openai-prod",
  "driver":         "openai",
  "credential_ref": "openai-main",
  "enabled":        true,
  "created_at":     "2026-06-25T10:00:00Z",
  "updated_at":     "2026-06-25T10:00:00Z"
}
```

### Custom OpenAI-compatible providers

For any endpoint that speaks the OpenAI chat-completions wire format, use
`driver: "custom_openai"` and supply the base URL in `config`:

```bash
curl -s -X POST http://localhost:8080/api/llm/providers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":           "internal-vllm",
    "driver":         "custom_openai",
    "credential_ref": "vllm-token",
    "enabled":        true,
    "config": {
      "base_url": "http://vllm.internal:8000"
    }
  }'
```

### Multiple keys per provider (weighted routing and fallback)

A single provider can hold multiple credential entries — useful when you have primary
and fallback keys, or want to spread load across regional endpoints. Add additional keys
with `POST /api/llm/providers/{name}/keys`:

```bash
curl -s -X POST \
  "http://localhost:8080/api/llm/providers/openai-prod/keys" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "credential_ref": "openai-secondary",
    "weight":         0.5,
    "enabled":        true
  }'
```

The `weight` field drives weighted routing across multiple keys. When a key becomes
unavailable, the engine falls back to the next enabled key automatically and emits an
`llm.fallback_used` audit event.

## Step 3 — Create a model alias

A model alias maps a tenant-scoped name to a specific provider and upstream model id.
Clients always address the alias; the actual provider model is an operator concern.

```bash
curl -s -X POST http://localhost:8080/api/llm/models \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "alias":          "gpt-4",
    "provider_name":  "openai-prod",
    "provider_model": "gpt-4o",
    "capabilities":   ["chat", "tool_use"],
    "enabled":        true
  }'
```

Valid capability values: `chat`, `completion`, `embedding`, `moderation`, `tool_use`.

You can confirm the alias is visible on the OpenAI models list endpoint:

```bash
curl -s http://localhost:8080/v1/models \
  -H "Authorization: Bearer $INVOKE_TOKEN" | jq '.data[].id'
```

## Step 4 — Issue a Virtual Key

Applications authenticate with Virtual Keys. A Virtual Key carries the `llm:invoke`
scope required by the chat-completions endpoint, and optionally constrains which
providers and model aliases the holder can reach.

```bash
# Create a Virtual Key scoped to LLM invocation only.
curl -s -X POST http://localhost:8080/api/governance/virtual-keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":            "app-prod-llm",
    "scopes":          ["llm:invoke"],
    "model_allowlist": ["gpt-4"],
    "enabled":         true
  }'
```

The response includes a one-time `secret` field — the token your application presents.
Store it; the gateway never returns it again. See [Virtual Keys](/concepts/virtual-keys)
for rotation, revocation, and budget binding.

## Step 5 — Make the call

With a provider registered, an alias defined, and a Virtual Key in hand, you can make
an OpenAI-compatible chat completion:

```bash
curl -s -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $VK_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [
      {"role": "system",  "content": "You are a concise assistant."},
      {"role": "user",    "content": "What is a gateway?"}
    ]
  }'
```

Example response:

```json
{
  "id":      "chatcmpl-portico",
  "object":  "chat.completion",
  "created": 1750848000,
  "model":   "gpt-4",
  "choices": [
    {
      "index":         0,
      "message":       {"role": "assistant", "content": "A gateway is a ..."},
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens":     28,
    "completion_tokens": 42,
    "total_tokens":      70
  }
}
```

::: info Model field echoes the alias
The `model` field in the response echoes the alias the client sent, not the upstream
model id. This keeps client code stable even when the operator remaps an alias to a
different underlying model.
:::

### Using the official OpenAI SDK

Any client that accepts a configurable base URL works unchanged:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key=vk_secret,        # your Portico Virtual Key
)

response = client.chat.completions.create(
    model="gpt-4",            # tenant-scoped alias
    messages=[
        {"role": "user", "content": "What is a gateway?"}
    ],
)
print(response.choices[0].message.content)
```

## Streaming

Set `"stream": true` to receive a Server-Sent Events stream. The wire format follows
the OpenAI `chat.completion.chunk` shape. Portico emits a heartbeat comment every 15
seconds on long-running generations and always terminates the stream with a `[DONE]`
event.

```bash
curl -sN -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $VK_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Count to five, one word per line."}
    ]
  }'
```

Each SSE event carries a `data:` line with a JSON object:

```jsonc
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1750848000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"One"},"finish_reason":null}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":1750848000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"\n"},"finish_reason":null}]}

// ... additional chunks ...

data: [DONE]
```

## Embeddings

The embeddings endpoint follows the same pattern. Register a provider and model alias
with `"embedding"` in the capabilities list, then:

```bash
curl -s -X POST http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer $VK_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "embed-small",
    "input": ["The quick brown fox", "jumps over the lazy dog"]
  }'
```

The `input` field accepts a single string or an array of strings.

## Checking quotas

Each tenant has per-minute and per-day limits on requests and tokens. Exceeded limits
return `429` with an error slug of `quota_exceeded`. You can inspect and update the
limits for the current tenant:

```bash
# Read current quota
curl -s http://localhost:8080/api/llm/quota \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Update quota
curl -s -X PUT http://localhost:8080/api/llm/quota \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "requests_per_minute": 120,
    "tokens_per_minute":   50000,
    "tokens_per_day":      1000000,
    "cost_usd_per_day":    25.00
  }'
```

A value of `0` on any field means unlimited for that dimension.

## Provider health

The health endpoint returns the engine's live view of every configured provider:

```bash
curl -s http://localhost:8080/api/llm/health \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

```json
{
  "providers": [
    {
      "provider": "openai-prod",
      "driver":   "openai",
      "healthy":  true,
      "detail":   ""
    }
  ]
}
```

## Common error slugs

| Slug                      | HTTP | Meaning                                                          |
|---------------------------|------|------------------------------------------------------------------|
| `model_unknown`           | 404  | No enabled model alias with that name exists for the tenant.     |
| `provider_unavailable`    | 502  | The upstream provider returned an error or is unreachable.       |
| `quota_exceeded`          | 429  | The tenant has hit its per-minute or per-day request/token limit.|
| `policy_denied_llm`       | 403  | A policy rule blocked the call before it reached the provider.   |
| `unsupported_capability`  | 422  | The model does not declare the requested capability (e.g. tools).|
| `upstream_error`          | 502  | The upstream provider returned a non-successful response.        |
| `llm_not_configured`      | 503  | The LLM gateway is not wired in the current binary configuration.|

::: warning Credentials never in errors
Upstream error messages occasionally echo request fragments. Portico's redactor scrubs
known token shapes (`sk-...`, `anthropic-...`, `xai-...`, and similar patterns) from
every error response and audit event before they are persisted or returned to the
client.
:::

## Next steps

The four-step setup above covers a single tenant pointing at one provider. For
production deployments you will want to:

- Bind Virtual Keys to [Agent Profiles](/concepts/agent-profiles) to enforce which
  models, tools, and skills each consumer can reach.
- Set up [Hierarchical Budgets](/concepts/hierarchical-budgets) to give individual
  customers or teams their own spend limits within the tenant envelope.
- Enable the [Semantic Cache](/concepts/semantic-cache) to reduce upstream costs on
  repetitive prompts.
- Review [LLM Routing](/concepts/llm-routing) to understand how multi-key weighted
  routing and fallback work in more detail.

## Related

- [LLM Gateway concept](/concepts/llm-gateway) — architecture, engine seam, audit and
  observability details.
- [LLM Providers concept](/concepts/llm-providers) — built-in driver reference, custom
  OpenAI-compatible provider templates, per-provider network config.
- [LLM Routing concept](/concepts/llm-routing) — alias resolution, weighted keys,
  fallback, policy redirects.
- [Manage providers guide](/guides/manage-providers) — step-by-step operator walkthrough
  for adding, testing, and rotating providers.
- [Virtual Keys concept](/concepts/virtual-keys) — how VKs are issued, rotated, and
  scoped; the one-time secret contract.
- [Hierarchical Budgets concept](/concepts/hierarchical-budgets) — per-VK, per-team,
  per-customer, and per-tenant cost caps.
- [Semantic Cache concept](/concepts/semantic-cache) — similarity-based response reuse,
  cache drivers, TTL, and invalidation.
- [Getting started overview](/getting-started/) — prerequisite steps before this guide.
