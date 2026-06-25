# REST API

Portico exposes a single HTTP listener that carries every surface: the northbound MCP transport, the northbound A2A transport, the OpenAI-compatible LLM gateway, and the operator REST API. Every route below is served from the same binary and the same TCP port.

This page is a route-level reference. For the concepts behind authentication, tenancy, and policy, see the linked concept pages. For CLI invocations and configuration keys, see [Configuration](/reference/configuration) and the [CLI reference](/reference/cli).

---

## Authentication model

All routes except `/healthz`, `/readyz`, and `/api/gateway/info` sit inside an authentication middleware group. There are two auth modes:

| Mode | How to enable | Credential accepted |
|------|---------------|---------------------|
| **JWT** | Set `auth.jwks_url` in config | `Authorization: Bearer <jwt>` — RS256/RS384/RS512/ES256/ES384/ES512 only |
| **Virtual Key** | Governance VKs wired | `Authorization: Bearer pk-portico-<id>.<secret>` |
| **Dev mode** | `portico dev` (localhost only) | No credential required; synthetic `dev` tenant injected |

JWT validation extracts the tenant identity from a configurable claim (default `tenant_id`) and resolves scopes from a second claim (default `scope`). Virtual Keys are validated via HMAC-SHA256 against the stored salt; the plaintext secret is never persisted.

Scope requirements are noted per-route below. The `admin` scope satisfies every narrower scope requirement.

See [Authentication](/concepts/authentication) for the full token flow, JWKS rotation, and Virtual Key lifecycle.

---

## Response envelope

All responses are `Content-Type: application/json`. Errors use a consistent structure:

```json
{
  "error": "not_found",
  "message": "server not found",
  "details": { "id": "my-server" }
}
```

`error` is a snake_case machine-readable code. `details` is present only when it carries actionable context. HTTP status codes follow standard semantics; 204 means no body.

---

## Health and discovery

These endpoints are unauthenticated and always available.

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/healthz` | Liveness probe. Returns `{"status":"ok"}`. |
| `GET` | `/readyz` | Readiness probe. Returns `{"status":"ready","version":"…","commit":"…"}`. |
| `GET` | `/api/gateway/info` | Public connection facts: bind address, MCP path, auth mode, JWKS URL, and version. No secrets are included. |

**`GET /api/gateway/info` response shape:**

```json
{
  "bind": "0.0.0.0:8080",
  "mcp_path": "/mcp",
  "version": "v0.5.0",
  "build_commit": "abc1234",
  "dev_mode": false,
  "auth": {
    "mode": "jwt",
    "issuer": "https://auth.example.com",
    "audiences": ["portico"],
    "jwks_url": "https://auth.example.com/.well-known/jwks.json",
    "tenant_claim": "tenant_id",
    "scope_claim": "scope"
  }
}
```

---

## MCP northbound transport

The MCP gateway is not a REST surface; it speaks JSON-RPC 2.0 over HTTP+SSE per the MCP spec. The transport is mounted on `/mcp` inside the authentication group.

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/mcp` | Send a JSON-RPC request or notification. Opens the SSE stream for server-initiated messages. |
| `GET`  | `/mcp` | Subscribe to the SSE event stream for an established session. |
| `DELETE` | `/mcp` | Terminate a session. |

For the full JSON-RPC method catalogue see [MCP Methods](/reference/mcp-methods). For northbound and southbound architecture details see [MCP Northbound](/concepts/mcp-northbound) and [MCP Southbound](/concepts/mcp-southbound).

---

## Servers (`/v1/servers` and `/api/servers`)

Both prefix families expose the server registry. The `/v1/servers` routes are the canonical API; `/api/servers` adds the Phase 9 extended management surface (logs, health, PATCH, restart). Scope required: tenant-level auth; no extra scope beyond authentication.

### `/v1/servers`

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/v1/servers` | 200 | List all registered MCP servers for the authenticated tenant. Returns an array; empty array when none. |
| `POST` | `/v1/servers` | 201 / 200 | Register or upsert a server. 201 on creation, 200 if the ID already existed. Body: `ServerSpec`. |
| `GET` | `/v1/servers/{id}` | 200 / 404 | Get one server by ID. Response includes live `instances` array. |
| `PUT` | `/v1/servers/{id}` | 200 / 404 | Replace a server spec. 404 if the ID does not exist. |
| `DELETE` | `/v1/servers/{id}` | 204 / 404 | Delete a server and all its data. |
| `POST` | `/v1/servers/{id}/reload` | 202 | Drain and restart all running instances for this server. |
| `POST` | `/v1/servers/{id}/enable` | 200 | Enable the server (sets `enabled: true`). |
| `POST` | `/v1/servers/{id}/disable` | 200 | Disable the server (sets `enabled: false`). |
| `GET` | `/v1/servers/{id}/instances` | 200 / 404 | List active runtime instances for the server. |

### `/api/servers` (extended management)

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/api/servers` | 200 | Same as `/v1/servers` with substrate metadata (capabilities, skills count, policy state, auth state). |
| `POST` | `/api/servers` | 201 | Create a server; Console-facing variant with richer validation response. |
| `GET` | `/api/servers/{id}` | 200 / 404 | Get one server with substrate metadata. |
| `PUT` | `/api/servers/{id}` | 200 / 404 | Replace a server spec. |
| `PATCH` | `/api/servers/{id}` | 200 / 404 | Partial update (only supplied fields are changed). |
| `DELETE` | `/api/servers/{id}` | 204 / 404 | Delete a server. |
| `POST` | `/api/servers/{id}/restart` | 202 | Drain and restart all instances. |
| `GET` | `/api/servers/{id}/logs` | 200 (SSE) | Stream recent log lines for a server (Server-Sent Events). |
| `GET` | `/api/servers/{id}/health` | 200 | Last-known health state for all instances of the server. |
| `GET` | `/api/servers/{id}/activity` | 200 | Recent audit activity for this server entity. |

**Server response fields (selected):**

```json
{
  "id": "github-mcp",
  "tenant_id": "acme",
  "display_name": "GitHub MCP",
  "transport": "http",
  "runtime_mode": "shared",
  "status": "running",
  "enabled": true,
  "capabilities": { "tools": 12, "resources": 0, "prompts": 0 },
  "skills_count": 3,
  "policy_state": "rules_present",
  "spec": { "…": "…" }
}
```

See [MCP Registry](/concepts/mcp-registry) for the full `ServerSpec` schema.

---

## Audit events (`/v1/audit/events`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/v1/audit/events` | tenant | Query tenant-scoped audit events. Accepts `?limit`, `?offset`, `?event_type`, `?since`, `?until` query parameters. Admin scope can add `?tenant_id=*` to query across tenants. |

Response: `{"events": [...], "total": N}`

See [Audit](/concepts/audit) for event schemas and retention.

---

## Approvals (`/v1/approvals`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/v1/approvals` | tenant | List pending and decided approvals for the tenant. |
| `GET` | `/v1/approvals/{id}` | tenant | Get one approval by ID. |
| `POST` | `/v1/approvals/{id}/approve` | admin | Mark an approval as approved. Wakes the waiting tool call. |
| `POST` | `/v1/approvals/{id}/deny` | admin | Mark an approval as denied. The tool call receives an error response. |

**Approval object fields:**

```json
{
  "id": "apr_…",
  "tenant_id": "acme",
  "session_id": "ses_…",
  "tool": "github:create_issue",
  "args_summary": "repo=acme/backend, title=Fix #42",
  "risk_class": "write",
  "status": "pending",
  "created_at": "2026-01-01T00:00:00Z",
  "expires_at": "2026-01-01T00:10:00Z"
}
```

See [Approvals](/concepts/approvals) for the full approval lifecycle.

---

## Catalog and snapshots (`/v1/catalog`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `POST` | `/v1/catalog/resolve` | tenant | Resolve a tool/resource/prompt name against the current catalog for the session. |
| `GET` | `/v1/catalog/snapshots` | tenant | List catalog snapshots taken for this tenant. |
| `GET` | `/v1/catalog/snapshots/{id}` | tenant | Get a specific snapshot. |
| `GET` | `/v1/catalog/snapshots/{a}/diff/{b}` | tenant | Compute the diff between two snapshots (added/removed/changed items). |
| `GET` | `/v1/sessions/{session_id}/snapshot` | tenant | Get the snapshot bound to a specific MCP session. |

See [Catalog and Sessions](/concepts/catalog-and-sessions) for snapshot semantics.

---

## Skills (`/v1/skills`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/v1/skills` | tenant | List Skill Packs available to the tenant. |
| `GET` | `/v1/skills/{id}` | tenant | Get a Skill Pack by ID. |
| `GET` | `/v1/skills/{id}/manifest.yaml` | tenant | Download the raw manifest for a Skill Pack. |
| `POST` | `/v1/skills/{id}/enable` | tenant | Enable a Skill Pack globally for the tenant. |
| `POST` | `/v1/skills/{id}/disable` | tenant | Disable a Skill Pack globally for the tenant. |
| `GET` | `/v1/sessions/{session_id}/skills` | tenant | List Skill Packs and their enabled state for a session. |
| `POST` | `/v1/sessions/{session_id}/skills/enable` | tenant | Enable a Skill Pack for a specific session. |
| `POST` | `/v1/sessions/{session_id}/skills/disable` | tenant | Disable a Skill Pack for a specific session. |

See [Skill Packs](/concepts/skill-packs) and [Skill Sources](/concepts/skill-sources).

---

## Skill sources (`/api/skill-sources`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/skill-sources` | tenant | List registered skill sources. |
| `POST` | `/api/skill-sources` | tenant | Register a new skill source. |
| `GET` | `/api/skill-sources/{name}` | tenant | Get a skill source by name. |
| `PUT` | `/api/skill-sources/{name}` | tenant | Replace a skill source registration. |
| `DELETE` | `/api/skill-sources/{name}` | tenant | Remove a skill source. |
| `POST` | `/api/skill-sources/{name}/refresh` | tenant | Trigger an immediate reload of all Skill Packs from this source. |
| `GET` | `/api/skill-sources/{name}/packs` | tenant | List Skill Packs discovered from a specific source. |

### Authored skills (`/api/skills/authored`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/skills/authored` | tenant | List tenant-authored Skill Packs. |
| `POST` | `/api/skills/authored` | tenant | Create a new authored skill. |
| `GET` | `/api/skills/authored/{id}` | tenant | Get the active version of an authored skill. |
| `GET` | `/api/skills/authored/{id}/versions` | tenant | List all versions. |
| `GET` | `/api/skills/authored/{id}/versions/{v}` | tenant | Get a specific version. |
| `PUT` | `/api/skills/authored/{id}/versions/{v}` | tenant | Update a draft version. |
| `POST` | `/api/skills/authored/{id}/versions/{v}/publish` | tenant | Publish a version (makes it the active version). |
| `POST` | `/api/skills/authored/{id}/versions/{v}/archive` | tenant | Archive a version. |
| `DELETE` | `/api/skills/authored/{id}/versions/{v}` | tenant | Delete a draft version. |
| `POST` | `/api/skills/validate` | tenant | Validate a skill manifest without persisting it. Returns validation errors if any. |

---

## MCP resources and prompts (`/v1/resources`, `/v1/prompts`, `/v1/apps`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/v1/resources` | tenant | List aggregated MCP resources across all connected servers. |
| `GET` | `/v1/resources/templates` | tenant | List resource templates. |
| `GET` | `/v1/resources/*` | tenant | Read a resource by URI. |
| `GET` | `/v1/prompts` | tenant | List aggregated MCP prompts. |
| `POST` | `/v1/prompts/{name}` | tenant | Invoke a prompt by name with arguments. |
| `GET` | `/v1/apps` | tenant | List `ui://` MCP App resources indexed by the Apps registry. |

---

## LLM gateway — OpenAI-compatible surface (`/v1/…`)

The LLM gateway is the OpenAI-compatible northbound for chat completions, embeddings, and model discovery. Scope `llm:invoke` (or `admin`) is required for inference calls.

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `POST` | `/v1/chat/completions` | `llm:invoke` | Chat completion. Accepts the OpenAI request shape (`model`, `messages`, `temperature`, `max_tokens`, `stream`). The `model` field names a tenant-scoped alias. |
| `POST` | `/v1/embeddings` | `llm:invoke` | Embeddings. Accepts `model` (alias) and `input` (string or array of strings). |
| `GET` | `/v1/models` | `llm:invoke` | List available model aliases in the OpenAI `{"object":"list","data":[…]}` shape. |

**Chat completions request:**

```json
{
  "model": "my-gpt4",
  "messages": [
    { "role": "system", "content": "You are a helpful assistant." },
    { "role": "user", "content": "Summarise this PR." }
  ],
  "temperature": 0.7,
  "stream": false
}
```

**Chat completions response:**

```json
{
  "id": "chatcmpl-…",
  "object": "chat.completion",
  "created": 1719100000,
  "model": "my-gpt4",
  "choices": [
    { "index": 0, "message": { "role": "assistant", "content": "…" }, "finish_reason": "stop" }
  ],
  "usage": { "prompt_tokens": 42, "completion_tokens": 120, "total_tokens": 162 }
}
```

See [LLM Gateway](/concepts/llm-gateway), [LLM Providers](/concepts/llm-providers), and [LLM Routing](/concepts/llm-routing).

---

## LLM admin (`/api/llm/…`)

### Providers

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/llm/providers` | tenant | List configured LLM providers. |
| `POST` | `/api/llm/providers` | admin | Create a provider. |
| `GET` | `/api/llm/providers/{name}` | tenant | Get a provider. |
| `PUT` | `/api/llm/providers/{name}` | admin | Replace a provider. |
| `DELETE` | `/api/llm/providers/{name}` | admin | Delete a provider. |
| `GET` | `/api/llm/providers/{name}/keys` | admin | List weighted API keys for a provider. |
| `POST` | `/api/llm/providers/{name}/keys` | admin | Add a weighted API key. |
| `DELETE` | `/api/llm/providers/{name}/keys/{keyID}` | admin | Remove a weighted API key. |

### Model aliases

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/llm/models` | tenant | List model aliases. |
| `POST` | `/api/llm/models` | admin | Create a model alias. |
| `GET` | `/api/llm/models/{alias}` | tenant | Get a model alias. |
| `PUT` | `/api/llm/models/{alias}` | admin | Replace a model alias. |
| `DELETE` | `/api/llm/models/{alias}` | admin | Delete a model alias. |

### Health, quota, costs, and cache

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/llm/health` | tenant | Per-provider liveness as seen by the engine. |
| `GET` | `/api/llm/quota` | tenant | Get the per-tenant token quota row. |
| `PUT` | `/api/llm/quota` | admin | Set the per-tenant token quota. |
| `GET` | `/api/llm/costs` | tenant | Daily token cost rollups for the tenant. |
| `GET` | `/api/llm/costs/prices` | tenant | Global price book (cost per token per model). |
| `PUT` | `/api/llm/costs/prices` | admin | Update a price book entry. |
| `GET` | `/api/llm/sessions` | tenant | List LLM chat sessions (conversation history). |
| `GET` | `/api/llm/sessions/{chat_id}` | tenant | Get a full LLM chat session transcript. |
| `GET` | `/api/llm/cache/config` | tenant | Semantic cache configuration (driver, scope, TTL, threshold). |
| `GET` | `/api/llm/cache/stats` | tenant | Semantic cache hit/miss/eviction statistics. |
| `POST` | `/api/llm/cache/invalidate` | admin | Invalidate the semantic cache for the tenant. |

See [Semantic Cache](/concepts/semantic-cache) for cache behaviour.

---

## Code Mode (`/api/code-mode/…`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/code-mode/executions` | admin | List Starlark execution history for the tenant. |
| `GET` | `/api/code-mode/savings` | admin | Token-savings ROI rollup (cost avoided by Code Mode vs. direct tool calls). |
| `GET` | `/api/code-mode/files` | admin | List the stub file index available to Code Mode snippets. |
| `GET` | `/api/code-mode/files/read` | admin | Read a stub file by path (query param `path=…`). |
| `POST` | `/api/code-mode/run` | admin | Execute a Starlark snippet against the sandbox. Returns the result and token usage. |

See [Code Mode](/concepts/code-mode) and [Code Mode Savings](/concepts/code-mode-savings).

---

## Agent Profiles (`/api/agent-profiles`)

Agent Profiles define what an agent or consumer can see and call: which MCP servers, tools, skills, model aliases, A2A peers, and bridge routes are available. Scope: admin required for all operations.

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/api/agent-profiles` | 200 | List all agent profiles for the tenant. Note: the synthesised default profile is not returned. |
| `POST` | `/api/agent-profiles` | 201 | Create a profile. The server generates the ID (`ap_<hex>`); any client-supplied `id` is ignored. |
| `GET` | `/api/agent-profiles/{id}` | 200 / 404 | Get a profile. |
| `PUT` | `/api/agent-profiles/{id}` | 200 / 404 | Replace a profile. |
| `DELETE` | `/api/agent-profiles/{id}` | 204 / 404 | Delete a profile. The default profile is a code construct and cannot be deleted. |
| `GET` | `/api/agent-profiles/{id}/surface` | 200 | Materialised surface: the live intersection of the profile's allowlists with the actually registered servers, models, and peers. Includes `profile_id` and `is_default`. |
| `PUT` | `/api/agent-profiles/{id}/bindings/{sub}` | 204 | Bind a JWT `sub` claim to this profile. Idempotent. |
| `DELETE` | `/api/agent-profiles/{id}/bindings/{sub}` | 204 | Remove a JWT subject binding. |

**Profile object fields:**

```json
{
  "id": "ap_0a1b2c3d4e5f…",
  "name": "CI Agent",
  "description": "Read-only CI pipeline agent",
  "allowed_mcp_servers": ["github", "jira"],
  "allowed_tools": [],
  "allowed_skills": ["code-review"],
  "allowed_model_aliases": ["gpt-4o-mini"],
  "allowed_a2a_peers": [],
  "allowed_a2a_tasks": [],
  "mcp_to_a2a_bridges": [],
  "a2a_to_mcp_bridges": [],
  "scopes": ["mcp:call", "llm:invoke"],
  "enabled": true
}
```

See [Agent Profiles](/concepts/agent-profiles).

---

## Policy rules (`/api/policy/…`)

| Method | Path | Scope | Purpose |
|--------|------|-------|---------|
| `GET` | `/api/policy/rules` | tenant | List the tenant's policy rule set. Returns `{"rules":[…]}` — never null. |
| `PUT` | `/api/policy/rules` | tenant | Atomically replace the entire rule set. All rules are validated before any write occurs. |
| `POST` | `/api/policy/rules` | tenant | Append a single rule. |
| `PUT` | `/api/policy/rules/{id}` | tenant | Replace a single rule by ID. |
| `DELETE` | `/api/policy/rules/{id}` | tenant | Delete a rule by ID. |
| `POST` | `/api/policy/dry-run` | tenant | Evaluate a hypothetical tool call against the current rules without executing it. Returns `allow` or `deny` with matching rule details. |
| `GET` | `/api/policy/activity` | tenant | Recent policy decision audit log for the tenant. |

See [Policy](/concepts/policy).

---

## Playground (`/api/playground/…`)

The interactive Playground lets operators exercise the live catalog — send tool calls, inspect results, and save cases for replay — without writing client code.

### Sessions

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/playground/sessions` | Start a playground session. Body: `{"snapshot_id":"…","runtime_override":"…","scopes":["…"]}`. Returns a session DTO. |
| `GET` | `/api/playground/sessions/{sid}` | Get a playground session. |
| `DELETE` | `/api/playground/sessions/{sid}` | End and discard a session. |
| `GET` | `/api/playground/sessions/{sid}/catalog` | Snapshot-bound catalog for the session. |
| `GET` | `/api/playground/sessions/{sid}/correlation` | Trace correlation data for recent calls in the session. |
| `POST` | `/api/playground/sessions/{sid}/skills/{skill_id}/enable` | Enable a skill for this session. |
| `POST` | `/api/playground/sessions/{sid}/skills/{skill_id}/disable` | Disable a skill for this session. |

### Calls (within a session)

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/playground/sessions/{sid}/calls` | Issue a tool call in the session. Body: `{"tool":"server:tool","arguments":{…}}`. Returns a call envelope with a call ID. |
| `GET` | `/api/playground/sessions/{sid}/calls/{cid}/stream` | Stream the result of a pending call (SSE). |
| `POST` | `/api/playground/sessions/{sid}/calls/{cid}/replay` | Replay a previous call (requires a saved case). |

### Saved cases and runs

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/playground/cases` | List saved test cases. |
| `POST` | `/api/playground/cases` | Save a new test case. |
| `GET` | `/api/playground/cases/{id}` | Get a test case. |
| `PUT` | `/api/playground/cases/{id}` | Update a test case. |
| `DELETE` | `/api/playground/cases/{id}` | Delete a test case. |
| `GET` | `/api/playground/cases/{id}/runs` | List replay run history for a test case. |
| `POST` | `/api/playground/cases/{id}/replay` | Replay a saved case, producing a new run record. |
| `GET` | `/api/playground/runs/{run_id}` | Get a specific run record. |
| `GET` | `/api/playground/runs/{run_id}/correlation` | Trace correlation data for a run. |

See [Playground](/concepts/playground).

---

## Session inspector and bundles (`/api/sessions/…`)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/sessions/imported` | List sessions previously imported from bundles. |
| `POST` | `/api/sessions/import` | Import a session bundle (multipart/JSON upload). |
| `GET` | `/api/sessions/{sid}/bundle` | Download or inspect a session bundle. |
| `POST` | `/api/sessions/{sid}/export` | Export a session to a portable bundle. |
| `GET` | `/api/spans` | Query stored trace spans. Accepts `?session_id`, `?limit`, `?offset`. |
| `GET` | `/api/audit/search` | Full-text search across the audit log. |

---

## Tenants (`/api/admin/tenants` and `/v1/admin/tenants`)

All tenant endpoints require `admin` scope. The `/api/admin/tenants` surface is the primary one; `/v1/admin/tenants` retains backward compatibility.

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/api/admin/tenants` | 200 | List all tenants. |
| `POST` | `/api/admin/tenants` | 201 / 200 | Create or upsert a tenant. Required fields: `id`, `display_name`, `plan`. |
| `GET` | `/api/admin/tenants/{id}` | 200 / 404 | Get a tenant. |
| `PUT` | `/api/admin/tenants/{id}` | 200 / 404 | Replace a tenant. |
| `DELETE` | `/api/admin/tenants/{id}` | 204 / 404 | Archive a tenant (sets `status: archived`). Gated by an approval if the approval store is wired. |
| `POST` | `/api/admin/tenants/{id}/purge` | 204 | Hard-delete a tenant and all its data. Requires approval confirmation. |
| `GET` | `/api/admin/tenants/{id}/activity` | 200 | Audit activity for a tenant entity. |

**Tenant request body:**

```json
{
  "id": "acme",
  "display_name": "Acme Corp",
  "plan": "enterprise",
  "runtime_mode": "per_tenant",
  "max_concurrent_sessions": 100,
  "max_requests_per_minute": 500,
  "audit_retention_days": 90,
  "jwt_issuer": "https://auth.acme.com",
  "jwt_jwks_url": "https://auth.acme.com/.well-known/jwks.json"
}
```

See [Multi-Tenancy](/concepts/multi-tenancy).

---

## Secrets vault (`/api/admin/secrets` and `/v1/admin/secrets`)

Scope: `admin`. The vault stores per-tenant named secrets encrypted at rest. Plaintext is never returned by metadata endpoints; use the reveal flow for one-shot disclosure.

### `/api/admin/secrets` (primary)

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/api/admin/secrets` | 200 | List secret metadata for the tenant. Admin can add `?tenant=<id>` to query another tenant. Returns `[{"tenant_id","name","version","updated_at"}]`. |
| `POST` | `/api/admin/secrets` | 201 | Create a secret. Body: `{"name":"…","value":"…","tenant_id":"…"}`. |
| `GET` | `/api/admin/secrets/{name}` | 200 / 404 | Get secret metadata (no plaintext). |
| `PUT` | `/api/admin/secrets/{name}` | 200 | Update a secret value. Body: `{"value":"…"}`. |
| `DELETE` | `/api/admin/secrets/{name}` | 204 | Delete a secret. Gated by approval when the approval store is wired. |
| `POST` | `/api/admin/secrets/{name}/rotate` | 200 | Re-encrypt the secret under the current root key. |
| `POST` | `/api/admin/secrets/{name}/reveal` | 200 | Issue a short-lived reveal token. The response `{"token":"…","expires_at":"…"}` should be consumed immediately. |
| `GET` | `/api/admin/secrets/reveal/{token}` | 200 | Consume a reveal token once. Returns `{"tenant_id","name","value"}`. Invalid or expired tokens return 400. |
| `POST` | `/api/admin/secrets/rotate-root` | n/a | Reserved for root key rotation. In V1 this operation is admin-CLI only; the REST endpoint returns 501. |
| `GET` | `/api/admin/secrets/{name}/activity` | 200 | Audit activity for a secret entity. |

### `/v1/admin/secrets` (backward compatible)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/v1/admin/secrets` | List `(tenant_id, name)` pairs across all tenants. |
| `PUT` | `/v1/admin/secrets/{tenant}/{name}` | Write a secret for any tenant (flat path). |
| `DELETE` | `/v1/admin/secrets/{tenant}/{name}` | Delete a secret (flat path). |

See [Credentials Vault](/concepts/credentials-vault) and [OAuth Token Exchange](/concepts/oauth-token-exchange).

---

## Governance (`/api/governance/…`)

The governance surface manages the entities that carry budget and access constraints: customers, teams, virtual keys, and budgets. Scope: `admin` required for all operations.

### Virtual Keys

Virtual Keys are bearer tokens with narrowed scopes and allowlists. The `token` field (prefixed `pk-portico-`) is returned exactly once at creation and once per rotation; it is never retrievable again.

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/api/governance/virtual-keys` | 200 | List virtual keys (no secret fields). |
| `POST` | `/api/governance/virtual-keys` | 201 | Create a virtual key. Returns `{"virtual_key":{…},"token":"pk-portico-…"}`. |
| `GET` | `/api/governance/virtual-keys/{id}` | 200 / 404 | Get a virtual key (no secret). |
| `POST` | `/api/governance/virtual-keys/{id}/rotate` | 200 | Rotate the key. Returns the new `{"virtual_key":{…},"token":"pk-portico-…"}`. The old token stops authenticating immediately. |
| `DELETE` | `/api/governance/virtual-keys/{id}` | 204 | Revoke a virtual key. The token will be rejected with `401` on all future requests. |
| `GET` | `/api/governance/virtual-keys/{id}/budget` | 200 | Hierarchical budget view for the key — the complete chain from key → team → customer, with current consumption. Returns `{"levels":[…]}`. |

**Virtual Key create request:**

```json
{
  "name": "ci-runner",
  "scopes": ["llm:invoke"],
  "provider_allowlist": ["openai"],
  "model_allowlist": ["gpt-4o-mini"],
  "mcp_server_allowlist": [],
  "profile_id": "ap_…",
  "parent_kind": "team",
  "parent_id": "team_…"
}
```

### Customers and Teams

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/governance/customers` | List customers. |
| `POST` | `/api/governance/customers` | Create a customer. |
| `GET` | `/api/governance/customers/{id}` | Get a customer. |
| `PUT` | `/api/governance/customers/{id}` | Replace a customer. |
| `DELETE` | `/api/governance/customers/{id}` | Delete a customer. |
| `GET` | `/api/governance/teams` | List teams. |
| `POST` | `/api/governance/teams` | Create a team. |
| `GET` | `/api/governance/teams/{id}` | Get a team. |
| `PUT` | `/api/governance/teams/{id}` | Replace a team. |
| `DELETE` | `/api/governance/teams/{id}` | Delete a team. |

### Budgets

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/governance/budgets` | List budgets. |
| `POST` | `/api/governance/budgets` | Create a budget. Required fields: `scope_kind`, `scope_id`, `metric`, `period`, `limit_val`. |
| `GET` | `/api/governance/budgets/{id}` | Get a budget. |
| `PUT` | `/api/governance/budgets/{id}` | Replace a budget. |
| `DELETE` | `/api/governance/budgets/{id}` | Delete a budget. |

**Budget object:**

```json
{
  "id": "bud_…",
  "scope_kind": "team",
  "scope_id": "team_…",
  "metric": "tokens",
  "period": "monthly",
  "alignment": "rolling",
  "limit_val": 1000000,
  "enabled": true
}
```

See [Virtual Keys](/concepts/virtual-keys) and [Hierarchical Budgets](/concepts/hierarchical-budgets).

---

## A2A — peer registry and northbound transport

### Peer registry (`/api/a2a/peers`)

Scope: `admin` required. Peers are remote A2A endpoints that Portico can route outbound tasks to.

| Method | Path | Status | Purpose |
|--------|------|--------|---------|
| `GET` | `/api/a2a/peers` | 200 | List registered A2A peers for the tenant. |
| `POST` | `/api/a2a/peers` | 201 | Register a peer. Required fields: `name`, `endpoint`. |
| `GET` | `/api/a2a/peers/{id}` | 200 / 404 | Get a peer. |
| `PUT` | `/api/a2a/peers/{id}` | 200 / 404 | Replace a peer registration. |
| `DELETE` | `/api/a2a/peers/{id}` | 204 | Remove a peer. |
| `POST` | `/api/a2a/peers/{id}/refresh-card` | 200 / 502 | Fetch and persist the peer's agent card. Returns 502 when the peer endpoint is unreachable. |

**Peer request body:**

```json
{
  "name": "planning-agent",
  "endpoint": "https://planning.example.com/a2a",
  "egress_auth_ref": "planning-agent-key",
  "enabled": true
}
```

`egress_auth_ref` names a vault secret (tenant-scoped) holding the bearer credential Portico injects when dispatching to the peer.

### Northbound A2A transport

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/a2a/.well-known/agent.json` | Portico's agent card for discovery. Response follows the A2A agent card schema. The `name` field is `"Portico"`. |
| `POST` | `/a2a` | A2A JSON-RPC 2.0 endpoint. Accepts `message/send`, `tasks/get`, and `tasks/cancel`. Target peer is named via `params.metadata.portico_peer`. |

**`POST /a2a` envelope:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "messageId": "msg-1",
      "parts": [{ "kind": "text", "text": "Summarise open PRs." }]
    },
    "metadata": {
      "portico_peer": "planning-agent"
    }
  }
}
```

A2A error responses are carried in the JSON-RPC body (HTTP 200) following the JSON-RPC 2.0 error convention. The transport never bypasses the tenant auth envelope or Agent Profile enforcement.

See [A2A](/concepts/a2a) and [A2A Bridges](/concepts/a2a-bridges).

---

## Error codes reference

| Code | HTTP | Meaning |
|------|------|---------|
| `not_found` | 404 | The named resource does not exist for this tenant. |
| `unauthorized` | 401 | Missing or invalid credential. |
| `forbidden` | 403 | Credential is valid but lacks the required scope. |
| `invalid_json` | 400 | Request body could not be parsed. |
| `invalid_request` | 400 | Parsed body failed semantic validation. |
| `id_mismatch` | 400 | Path ID and body ID differ on a PUT. |
| `validation_failed` | 400 | Field-level validation error; check `details`. |
| `registry_unavailable` | 503 | The server registry is not configured in this build. |
| `vault_not_configured` | 503 | No vault key is set (`PORTICO_VAULT_KEY` missing). |
| `llm_not_configured` | 503 | LLM engine or stores are not wired. |
| `model_not_found` | 404 | The requested model alias does not exist. |
| `a2a_not_configured` | 503 | A2A peer store is not wired. |
| `agent_profiles_not_configured` | 503 | Agent profile store is not wired. |
| `approvals_not_configured` | 503 | Approval store is not wired. |
| `method_not_allowed` | 405 | Wrong HTTP method for the route. |

---

::: tip Dev mode shortcut
Run `portico dev` to start the server with no JWT requirement, a synthetic `dev` tenant, and `admin` scope injected automatically. All endpoints behave identically to production, making it safe to explore the API without a full IdP configuration.
:::

::: warning Approval-gated routes
Several destructive routes (`DELETE /api/admin/tenants/{id}`, `DELETE /api/admin/secrets/{name}`, `POST /api/admin/tenants/{id}/purge`, `POST /api/admin/secrets/rotate-root`) will return `202` with an `approval_request_id` when the approval store is wired and the operation matches a policy rule. Re-issue the request with `X-Approval-Token: <id>` after the approval is resolved. See [Approvals](/concepts/approvals).
:::

---

## Related

- [Authentication](/concepts/authentication) — JWT validation, JWKS rotation, Virtual Key bearer auth
- [Agent Profiles](/concepts/agent-profiles) — consumer entitlement and surface intersection
- [Virtual Keys](/concepts/virtual-keys) — bearer tokens with narrowed scopes and allowlists
- [MCP Methods](/reference/mcp-methods) — JSON-RPC method catalogue for the MCP transport
- [Configuration](/reference/configuration) — config keys that control which routes are mounted
- [Approvals](/concepts/approvals) — approval-gated mutations and the elicitation flow
