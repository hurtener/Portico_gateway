# Virtual Keys

A **Virtual Key** (VK) is a Portico-side credential — `pk-portico-<id>.<secret>`
— that an app or developer presents instead of a tenant JWT. It sub-divides a
tenant into per-app / per-developer / per-environment slots, each with its own
scopes, allowlists, budgets, and audit lineage. Revoking one VK touches no other
VK and not the parent tenant.

## Security model

- The secret is generated once and shown **once** (at create/rotate). Portico
  stores only a per-VK `salt` + `HMAC-SHA256(salt, secret)` — never the secret.
  A leaked database cannot reconstruct a usable key.
- A leaked key for one VK cannot authenticate as another: the presented secret
  is verified (constant-time) against *that* VK's stored HMAC, and the VK id
  determines the tenant.
- Resolution is the auth boundary: a presented VK carries no tenant; the
  globally-unique VK id resolves to its tenant (analogous to JWT → tenant).
  Malformed tokens are rejected without a DB hit; unknown/forged secrets return
  an **ambiguous** `401 vk_unknown` (no enumeration signal); a revoked key
  returns `401 vk_revoked`.

## Using a VK

```
Authorization: Bearer pk-portico-vk_ab12….<secret>
```

works on `/v1/chat/completions`, `/v1/embeddings`, and `/mcp/...`. The auth
middleware resolves it to a tenant identity carrying the VK's scopes, before the
JWT path, in both dev and production.

## What a VK carries

- **Scopes** — the standard scope set (e.g. `llm:invoke`, `mcp:call`). A request
  lacking a required scope is `403`.
- **Provider / model allowlists** — restrict which LLM providers/aliases the VK
  may call (`403 vk_scope_violation` otherwise). Empty = all.
- **MCP-server allowlist** — restrict which downstream MCP servers the VK reaches
  via `tools/call`; enforced in the dispatcher, so it also constrains Code Mode
  (a github-only VK cannot reach jira through the sandbox).
- **Agent Profile** (`profile_id`) — an optional Phase 14 Profile. The VK and the
  Profile **intersect** (most-restrictive-wins).
- **Budget parent** — a Team or Customer (or none) for hierarchical budgets.

## Lifecycle

- `POST /api/governance/virtual-keys` — create; returns the secret once.
- `POST /api/governance/virtual-keys/{id}/rotate` — new secret; the old one stops
  working immediately; budgets + audit history are preserved (no orphaning).
- `DELETE /api/governance/virtual-keys/{id}` — revoke (immediate; in-flight
  requests complete).
- `GET /api/governance/virtual-keys/{id}/budget` — live hierarchical headroom.

The Console manages all of this at **Governance → Virtual Keys**, surfacing the
secret exactly once in a copy-and-acknowledge modal. The offline CLI
(`portico governance ...`) manages customers/teams; VK issuance is REST/Console.

See also: [hierarchical-budgets](./hierarchical-budgets.md),
[agent-profiles](./agent-profiles.md).
