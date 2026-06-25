# Virtual Keys

A **Virtual Key** is a Portico-side credential in the form `pk-portico-<id>.<secret>`
that an application, developer, or environment presents to the gateway instead of a
full tenant JWT. Where a JWT authenticates the tenant as a whole, a Virtual Key
sub-divides that tenant into isolated slots, each carrying its own scopes, provider
and model allowlists, MCP server allowlist, optional Agent Profile binding, and
hierarchical budget parent. Revoking one Virtual Key has no effect on any other
Virtual Key or on the parent tenant.

Virtual Keys are issued and managed entirely within Portico. They are invisible to
downstream MCP servers and LLM providers: the gateway resolves a Virtual Key to a
tenant identity before forwarding any request outward.

---

## How Virtual Keys work

### Token format

Every Virtual Key token has the structure:

```
pk-portico-vk_<24-hex-id>.<base62-secret>
```

The `pk-portico-` prefix is fixed. The auth middleware uses it to route a Bearer
token to the Virtual Key resolver rather than the JWT validator — no ambiguity with
provider API keys, which use different prefixes. Malformed tokens (wrong prefix,
missing separator, empty segments) are rejected immediately, before any storage
lookup.

### What is stored

Portico stores **only** a per-VK `salt` (16 random bytes) and
`HMAC-SHA256(salt, secret)`. The secret itself is never written to disk. A stolen
database therefore cannot reconstruct a usable key, and a stolen key for one Virtual
Key cannot authenticate as another, because the presented secret is verified against
that specific VK's stored salt and HMAC.

Verification is performed in constant time via `crypto/subtle.ConstantTimeCompare`,
so the comparison does not leak timing information about how many bytes matched.

### Resolution flow

When the auth middleware sees `Authorization: Bearer pk-portico-…`, it:

1. Parses and validates the token format, rejecting malformed input before any DB access.
2. Looks up the stored VK row by the embedded ID.
3. Recomputes `HMAC-SHA256(salt, presented_secret)` and compares it in constant time.
4. Checks that `enabled = true` and `revoked_at IS NULL`.
5. Hydrates the VK's scopes, allowlists, attached Agent Profile ID, and budget parent
   into the request context.

Steps 2–5 are cached in-process with a 60-second TTL (bounded at 4 096 entries).
On the same instance, a revocation or rotation call immediately drops the cache
entry for the affected VK ID, so revocation takes effect within one cache TTL
across multiple instances (typically ≤ 60 s).

### Error codes

| Condition | HTTP status | Error code |
|---|---|---|
| Malformed token or unknown ID | `401` | `vk_unknown` |
| Valid token but key was revoked | `401` | `vk_revoked` |
| Scope missing (`llm:invoke` absent on `/v1/chat/completions`) | `403` | standard scope rejection |
| Provider or model outside the VK's allowlist | `403` | `vk_scope_violation` |
| Budget exceeded | `429` | `budget_exceeded` |

`vk_unknown` is intentionally ambiguous: it is returned for malformed tokens, IDs
that do not exist, and HMAC mismatches alike. This prevents an attacker from
enumerating valid VK IDs or tenants by observing whether the error is "not found"
versus "wrong secret."

---

## What a Virtual Key carries

### Scopes

Each Virtual Key holds a JSON array of scope strings. A request that requires a
scope the VK does not carry is rejected with a standard `403`. Common scopes:

| Scope | Grants |
|---|---|
| `llm:invoke` | Call `/v1/chat/completions` |
| `llm:embed` | Call `/v1/embeddings` |
| `mcp:call` | Dispatch tools via `/mcp/…` |
| `admin` | Governance CRUD (manage keys, budgets, etc.) |

### Provider and model allowlists

A VK may restrict which LLM providers and model aliases it can reach:

- **`provider_allowlist`** — a list of provider driver names (e.g. `anthropic`,
  `custom_openai`). An empty list means all providers are permitted.
- **`model_allowlist`** — a list of model aliases configured on the tenant. An empty
  list means all models are permitted.

A request whose resolved provider or alias is not in the allowlist receives
`403 vk_scope_violation`. The check is enforced in the gateway handler before any
call reaches the engine, so it also constrains Code Mode sessions that originate
LLM calls.

### MCP server allowlist

`mcp_server_allowlist` is a list of MCP server IDs the VK may reach through
`tools/call`. An empty list means all registered servers are accessible. The
allowlist is enforced in the tool dispatcher, which means it also constrains Code
Mode: a VK that lists only `["github"]` cannot reach a `jira` server through the
sandbox's tool dispatch path.

### Agent Profile binding

A VK may carry a `profile_id` pointing to an Agent Profile. When both a VK and a
Profile are present on a request, their allowlists are **intersected**: access is
granted only when both the VK and the Profile permit it (most-restrictive-wins
semantics). The Profile is the source of truth for consumer entitlement; the VK is
the credential lifecycle.

See [Agent Profiles](/concepts/agent-profiles) for the full entitlement model.

### Budget parent

A VK optionally belongs to a budget hierarchy via `parent_kind` and `parent_id`:

- `parent_kind: none` — the VK has only tenant-level budget parents.
- `parent_kind: team` — the VK rolls up to a Team, which itself may roll up to a Customer.
- `parent_kind: customer` — the VK rolls up directly to a Customer.

This produces a per-request scope chain of `vk → team → customer → tenant`. The
budget enforcer pre-checks each level from most-specific to least; the lowest level
that would be exceeded fires first.

See [Hierarchical Budgets](/concepts/hierarchical-budgets) for enforcement semantics.

---

## Lifecycle operations

All governance endpoints require the `admin` scope. The secret is returned
**exactly once** (at create and at rotate) and is never retrievable again.

### Create

```http
POST /api/governance/virtual-keys
Content-Type: application/json

{
  "name": "marketing-prod",
  "scopes": ["llm:invoke", "llm:embed"],
  "provider_allowlist": [],
  "model_allowlist": ["gpt-4o", "claude-sonnet"],
  "mcp_server_allowlist": [],
  "profile_id": "",
  "parent_kind": "team",
  "parent_id": "01HYZ..."
}
```

Response (the `token` field appears **only here**):

```json
{
  "virtual_key": {
    "id": "vk_3f8a2c...",
    "name": "marketing-prod",
    "parent_kind": "team",
    "parent_id": "01HYZ...",
    "scopes": ["llm:invoke", "llm:embed"],
    "provider_allowlist": [],
    "model_allowlist": ["gpt-4o", "claude-sonnet"],
    "mcp_server_allowlist": [],
    "enabled": true,
    "created_at": "2026-06-24T10:00:00Z"
  },
  "token": "pk-portico-vk_3f8a2c....ABCDEFabcde..."
}
```

Store the token value in your secrets manager immediately. Portico will never return
it again.

### Use

The token is sent as a standard Bearer credential:

```http
Authorization: Bearer pk-portico-<id>.<secret>
```

This header works on all gated surfaces:

- `/v1/chat/completions` (requires `llm:invoke`)
- `/v1/embeddings` (requires `llm:embed`)
- `/mcp/...` (requires `mcp:call`)

### List and inspect

```http
GET /api/governance/virtual-keys
GET /api/governance/virtual-keys/{id}
```

The response body contains the `VirtualKeyDTO`, which never includes the secret. The
`rotated_at` and `revoked_at` timestamps are included when set.

### Rotate

Rotation replaces the secret with a new one. The old secret stops working
immediately on the current instance (the resolver cache is purged synchronously for
that VK ID) and across all instances within the cache TTL. Budgets and audit history
attached to the VK ID are preserved — rotation does not orphan any records.

```http
POST /api/governance/virtual-keys/{id}/rotate
```

Response is the same shape as create: `virtual_key` plus a one-time `token`.

```bash
# CLI equivalent
portico governance vk rotate <id>
```

### Revoke

Revocation sets `enabled = false` and `revoked_at`. In-flight requests that have
already passed the auth check complete normally; all subsequent requests with that
token receive `401 vk_revoked`. Audit history is preserved.

```http
DELETE /api/governance/virtual-keys/{id}
```

```bash
portico governance vk revoke <id>
```

### Budget headroom

```http
GET /api/governance/virtual-keys/{id}/budget
```

Returns live hierarchical headroom for the VK and its parent chain:

```json
{
  "vk_id": "vk_3f8a2c...",
  "levels": [
    {
      "kind": "vk",
      "id": "vk_3f8a2c...",
      "metric": "cost_usd",
      "period": "1d",
      "used": 0.42,
      "limit": 5.00,
      "resets_at": "2026-06-25T00:00:00Z",
      "headroom_pct": 91.6
    },
    {
      "kind": "team",
      "id": "01HYZ...",
      "metric": "cost_usd",
      "period": "1M",
      "used": 312.10,
      "limit": 500.00,
      "resets_at": "2026-07-01T00:00:00Z",
      "headroom_pct": 37.6
    }
  ]
}
```

---

## Security model in depth

### One-time secret issuance

The Go service layer generates a cryptographically random 30-byte secret, encodes it
in a base62 alphabet (approximately 40 characters, ~240 bits of entropy), then
immediately computes `HMAC-SHA256(salt, secret)` and discards the raw secret. Only
`salt` and `hmac` are written to the `governance_virtual_keys` table. The plaintext
secret is returned once in the HTTP response and never touches persistent storage.

### Cross-tenant isolation

The VK ID is globally unique across all tenants. When the resolver looks up a VK by
ID, it retrieves the stored `tenant_id` from the row and attaches it to the request
context. There is no path by which a VK from one tenant can authenticate as another:
the HMAC is bound to a specific stored row, and that row encodes the owning tenant.
An attempt to use a VK from tenant A against tenant B's resources fails with
`401 vk_unknown` — the error is deliberately the same as "does not exist" to avoid
cross-tenant enumeration.

### No secret passthrough to downstream systems

The auth middleware sets `RawToken` to empty on the identity it builds from a VK
resolution. This means the gateway never forwards the VK secret as an
`Authorization` header to an MCP server or LLM provider. Downstream systems receive
the gateway's own credentials (managed via the [Credentials Vault](/concepts/credentials-vault)).

### Separation from provider API keys

Provider API keys begin with `sk-` (or similar provider-specific prefixes). Virtual
Keys begin with `pk-portico-`. The auth middleware distinguishes them by prefix
before any parsing, so there is no risk of a provider key being misrouted to the VK
resolver or vice versa.

---

## Intersection with Agent Profiles

An Agent Profile defines *what* a consumer may access (MCP servers, tools, Skill
Packs, model aliases, A2A peers). A Virtual Key is the *credential* that establishes
who the consumer is, with optional additional narrowing on top of the Profile.

When a VK has `profile_id` set:

1. The VK's own `model_allowlist`, `provider_allowlist`, and `mcp_server_allowlist`
   are checked first.
2. The attached Profile's `AllowedModelAliases`, `AllowedMCPServers`, and
   `AllowedTools` are checked in addition.
3. Both sets of checks must pass. Neither can expand what the other restricts.

A JWT caller without a VK is subject only to the Agent Profile resolved for that
principal (or the synthesised default profile if no profile is bound). The VK layer
is additive, not a replacement.

---

## Console and CLI

### Console

The Console surfaces Virtual Keys under **Governance → Virtual Keys**. The list page
shows each key's name, scopes, parent (Team / Customer / none), enabled status, and
last rotation timestamp. From the list page, operators can:

- Create a new VK (the secret is shown once in a copy-and-acknowledge modal).
- Open a VK's detail page to view its allowlists, attached Profile, and live budget
  headroom bars for every level in its hierarchy.
- Rotate a key (same one-time modal).
- Revoke a key.

The create and rotate flows will not let the operator dismiss the modal without
acknowledging they have saved the secret.

### CLI

```bash
# List all Virtual Keys for the current tenant
portico governance vk list

# Create a new key
portico governance vk create \
  --name "ci-pipeline" \
  --scopes llm:invoke,mcp:call \
  --model-allowlist claude-sonnet,gpt-4o

# Rotate a key
portico governance vk rotate vk_3f8a2c...

# Revoke a key
portico governance vk revoke vk_3f8a2c...
```

The CLI manage customers and teams (which are budget groupings); VK issuance goes
through the REST API or the Console so the one-time token is surfaced correctly.

---

## Enforcement guarantees

### Forbidden by construction

The following are enforced at the implementation level, not just by convention:

- **Secrets never stored in plaintext.** Only `salt + HMAC` are written. A test
  injects a known secret and asserts no plaintext representation appears anywhere in
  the database.
- **No VK issued without persisting `salt + HMAC` first.** The service writes the
  row before returning the token. A failed write means no token is returned.
- **Constant-time verification.** `crypto/subtle.ConstantTimeCompare` is used for
  all HMAC comparisons.
- **No cross-tenant budget aggregation.** Budget ledger rows are scoped to
  `(tenant_id, budget_id)`. Queries always filter by `tenant_id`.

### What revocation does and does not do

Revocation is soft: the row is marked `enabled = false` and `revoked_at` is
timestamped, but the row is not deleted. Audit events and budget ledger entries
attached to the VK ID remain intact for reporting and compliance purposes. In-flight
requests that already passed the auth check at the time of revocation complete
normally; this matches the behaviour of every other auth path in Portico.

---

## Related

- [Authentication](/concepts/authentication) — how Portico validates JWTs and Virtual Keys in the same middleware chain.
- [Agent Profiles](/concepts/agent-profiles) — the consumer entitlement surface that a VK may optionally bind to.
- [Hierarchical Budgets](/concepts/hierarchical-budgets) — cost and token budgets that roll up through VK, Team, Customer, and Tenant levels.
- [Credentials Vault](/concepts/credentials-vault) — how Portico stores and injects downstream provider credentials (distinct from Virtual Keys).
- [Audit](/concepts/audit) — every VK create, rotate, revoke, and scope violation produces an audit event.
- [Security Model](/concepts/security-model) — the full threat model covering credential isolation, tenant separation, and audit coverage.
- [REST API Reference](/reference/rest-api) — complete specification for all `/api/governance/virtual-keys` endpoints.
