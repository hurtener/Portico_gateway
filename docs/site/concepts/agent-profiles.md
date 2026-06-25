# Agent Profiles

An Agent Profile is the named, tenant-scoped object that answers one question: *what is this agent allowed to do in our environment?* It is the single source of truth for consumer entitlement in Portico — a unified binding that replaces the four-surface composition (scope sets, snapshot inclusion, Skill Pack enablement, and per-Virtual-Key allowlists) that would otherwise accumulate across a deployment.

Operators think in agents, not wire constructs: "Agent A talks to `github`, `jira`, and `sentry`; it can use the `code-review` Skill; it calls the `gpt-4o` alias; and it authenticates via two Virtual Keys — one for staging, one for production." A Profile makes that sentence a single first-class object stored in Portico's tenant-scoped database, manageable via REST, CLI, and the Console.

::: info Back-compatible by design
A principal with no Profile bound receives the **synthesised default profile** — the tenant's full surface. This means every existing client continues to work unchanged when you upgrade to a build that includes Agent Profiles. Restriction is opt-in: operators create a Profile and bind a principal to it; they never have to opt out of a restriction they did not request.
:::

---

## What a profile contains

| Field | Type | Meaning |
|---|---|---|
| `allowed_mcp_servers` | `[]string` | Subset of registered MCP server names the agent may reach |
| `allowed_tools` | `[]string` | Optional finer-grain allowlist of namespaced tool IDs (`server.tool`); empty = all tools in the allowed servers |
| `allowed_skills` | `[]string` | Subset of Skill Pack IDs the agent may enable |
| `allowed_model_aliases` | `[]string` | Subset of LLM model aliases the agent may call |
| `allowed_a2a_peers` | `[]string` | Subset of A2A peer names the agent may dispatch to |
| `allowed_a2a_tasks` | `[]string` | Optional finer-grain allowlist of namespaced A2A task IDs (`peer.task`); empty = all tasks of allowed peers |
| `scopes` | `[]string` | Scope set this profile grants when used as the effective scope set |
| `policy_bundle_ref` | `string` | Optional reference to a policy bundle that applies to this consumer |
| `mcp_to_a2a_bridges` | `[]MCPToA2ABridge` | MCP→A2A routing declarations for cross-protocol dispatch |
| `a2a_to_mcp_bridges` | `[]A2AToMCPBridge` | A2A→MCP routing declarations for inbound A2A tasks |

A profile is the **only** place where per-consumer entitlement lives. No surface (server registration, Skill Pack manifest, LLM provider config, A2A peer entry) carries its own consumer allowlist. Gates read the Profile from the request context; nothing else is consulted.

### Tool-level granularity

The `allowed_tools` field is optional and additive:

- **Empty** (the typical case): the agent may call any tool exposed by the servers in `allowed_mcp_servers`.
- **Non-empty**: the agent may call only the listed tools. Tools are namespaced as `server.tool` (e.g. `github.list_issues`). Listing a tool whose server is not in `allowed_mcp_servers` has no effect — the server check is always applied first.

The same two-level pattern applies to A2A: `allowed_a2a_peers` is the outer gate; `allowed_a2a_tasks` (namespaced `peer.task`) is the optional inner gate.

---

## How enforcement works

One middleware step, sitting after tenant + JWT/Virtual Key resolution and before the policy engine, resolves the caller's Profile and writes it into the request context. Every downstream surface reads it via `profiles.FromContext(ctx)` and calls the appropriate `Profile.Allows*` method.

### MCP tools/list filtering

`tools/list` returns tools only from the servers in `allowed_mcp_servers`, further intersected with `allowed_tools` when that list is non-empty. Tools from other servers are **absent from the JSON**, not merely hidden. An agent that does not appear to have a tool cannot attempt to call it.

### MCP tools/call rejection

A `tools/call` for a tool outside the profile's surface returns a structured JSON-RPC error with code `-32006` (`ErrAgentProfileViolation`) and a typed detail payload:

```json
{
  "code": -32006,
  "message": "agent profile violation",
  "data": {
    "profile_id": "ap_...",
    "tool": "sentry.delete_project",
    "reason": "tool_outside_profile"
  }
}
```

The rejection is also written to the audit log as an `agent_profile_violation` event carrying `profile_id`, `tool`, and `reason`.

### LLM /v1/\* alias gating

`/v1/chat/completions` (and `/v1/completions`, `/v1/embeddings`) reject a `model` value not in `allowed_model_aliases` with `HTTP 403` and the `agent_profile_violation` error slug. `/v1/models` returns only the aliases the caller's Profile permits.

### Skill Pack enablement

The Skills runtime enables only the packs listed in `allowed_skills`. A Skill not in the allowlist does not appear in `prompts/list` or as a `skill://` resource for that session.

### Catalog snapshot projection

When a session snapshot is generated, the catalog is projected through the Profile's `allowed_mcp_servers` and `allowed_tools`. Drift detection runs against the projected slice — a tool added to a server that is not in the Profile's allowlist does not produce a drift event for sessions belonging to that Profile.

### A2A peer gating

Outbound A2A dispatch calls `Profile.AllowsA2APeer` before routing to a peer, and `Profile.AllowsA2ATask` before dispatching a specific task. A rejection produces an audit event with `profile_id`, `peer`, and `reason: "peer_outside_profile"`.

---

## Intersection: most-restrictive wins

When multiple allowlist layers apply, the effective surface is always the **intersection** — the Profile is the headline, and Virtual Key allowlists or scope-implied surfaces may restrict it further but never widen it.

```
effective_servers = profile.allowed_mcp_servers
                  ∩ (vk.allowed_mcp_servers, when the VK carries its own allowlist)
                  ∩ (scope-implied surface from policy)

effective_tools   = (profile.allowed_tools if non-empty, else all tools in effective_servers)
                  ∩ (vk.allowed_tools, when the VK carries its own allowlist)
                  ∩ (policy-allowed tools)
```

Profiles narrow; they never broaden. A Profile carrying `scopes: [mcp:call]` for a principal whose JWT carries `scopes: [mcp:call, llm:invoke]` results in an effective scope of `[mcp:call]`. The Profile cannot grant a scope the JWT did not carry.

---

## The default profile

`profiles.DefaultProfile(tenantID)` is a code construct synthesised at runtime when no binding exists for the calling principal. It has `IsDefault: true` and every `Allows*` method returns `true`. It is **never persisted as a database row** and is never returned by `GET /api/agent-profiles`.

This is the back-compat seam: a deployment with no Profiles configured behaves identically to every prior release. Operators create Profiles and bind principals to them when they want to restrict access; they do not need to take any action to preserve the existing open surface.

---

## Configuration (static seeding)

Profiles can be declared in `portico.yaml` under the `agent_profiles:` key. The binary seeds them into the tenant-scoped store at boot; the operation is idempotent (matched by `tenant + name`, updated in place on restart). Hot reload is supported — a change to an `agent_profiles:` entry takes effect without a restart.

```yaml
agent_profiles:
  - name: "customer-support-eu"
    description: "EU customer support agent — read-only data access, GDPR-compliant model set"
    allowed_mcp_servers:
      - zendesk
      - intercom-eu
    allowed_tools:
      - zendesk.search_tickets
      - zendesk.get_ticket
      - intercom-eu.list_conversations
    allowed_skills:
      - "customer-support-triage"
    allowed_model_aliases:
      - "fast-summary"
    scopes:
      - mcp:call
      - llm:invoke
    bindings:
      - "agent-cs-eu@service-account"   # JWT sub values bound at boot

  - name: "engineering-debug"
    description: "Engineering debug agent — full registered surface, restricted by JWT"
    allowed_mcp_servers:
      - github
      - jira
      - sentry
      - datadog
    allowed_skills:
      - "code-review"
      - "incident-triage"
    allowed_model_aliases:
      - "gpt-4o"
      - "claude-3-5-sonnet"
    scopes:
      - mcp:call
      - llm:invoke
```

The `bindings` list is a convenience for static deployments: each entry is a JWT `sub` value that is bound to this Profile at startup (idempotent). Bindings can also be managed at runtime via the REST API and CLI.

An `agent_profiles:` block that is absent — or an empty list — means no Profiles are configured. Every authenticated request falls through to the default Profile. V1 and V1.5 behaviour is unchanged.

### Config field reference

| YAML key | Go field | Notes |
|---|---|---|
| `name` | `Name` | Required; unique within tenant |
| `description` | `Description` | Optional, operator-visible |
| `allowed_mcp_servers` | `AllowedMCPServers` | Server names from the registry |
| `allowed_tools` | `AllowedTools` | Namespaced `server.tool`; empty = all in allowed servers |
| `allowed_skills` | `AllowedSkills` | Skill Pack IDs |
| `allowed_model_aliases` | `AllowedModelAliases` | LLM alias strings |
| `scopes` | `Scopes` | Scope set granted by this Profile |
| `bindings` | `Bindings` | JWT `sub` values bound at boot |
| `enabled` | `Enabled` | Defaults to `true`; set to `false` to soft-disable |
| `tenant` | `Tenant` | Required when more than one tenant is configured |

---

## REST API

All endpoints are tenant-scoped — every read filters by the tenant derived from the request's JWT or Virtual Key.

```http
GET    /api/agent-profiles
POST   /api/agent-profiles
GET    /api/agent-profiles/{id}
PUT    /api/agent-profiles/{id}
DELETE /api/agent-profiles/{id}

GET    /api/agent-profiles/{id}/surface
PUT    /api/agent-profiles/{id}/bindings/{sub}
DELETE /api/agent-profiles/{id}/bindings/{sub}
```

`GET /api/agent-profiles/{id}/surface` returns the live materialised inventory — the intersection of the Profile's allowlists with the current registry state. A server added to the registry after the Profile was created but matching the Profile's allowlist appears immediately; no update to the Profile is required.

```json
{
  "profile_id": "ap_3e9f...",
  "is_default": false,
  "servers": ["github", "jira"],
  "tools": ["github.list_issues", "github.comment"],
  "skills": ["code-review"],
  "models": ["gpt-4o"]
}
```

### Error slugs

| Slug | HTTP status | Meaning |
|---|---|---|
| `agent_profile_violation` | 403 / JSON-RPC -32006 | Consumer attempted access outside profile surface |
| `agent_profile_unknown` | 404 | Named profile does not exist |

---

## CLI

The `portico agents` subcommand manages Profiles against a local SQLite database (useful for GitOps-style workflows or offline inspection).

```bash
# List all profiles for a tenant
portico agents list --tenant acme

# Inspect one profile
portico agents get --tenant acme --id ap_3e9f...

# Create a profile
portico agents create \
  --tenant acme \
  --name "customer-support-eu" \
  --servers "zendesk,intercom-eu" \
  --tools "zendesk.search_tickets,zendesk.get_ticket" \
  --skills "customer-support-triage" \
  --models "fast-summary" \
  --scopes "mcp:call,llm:invoke"

# Bind a JWT subject to a profile
portico agents bind --tenant acme --id ap_3e9f... --sub "agent-cs@service"

# Remove a binding
portico agents unbind --tenant acme --id ap_3e9f... --sub "agent-cs@service"

# Ask "would this profile allow this call?" — matches live dispatcher logic
portico agents test --tenant acme --id ap_3e9f... --tool github.delete_repo
portico agents test --tenant acme --id ap_3e9f... --alias gpt-4o
portico agents test --tenant acme --id ap_3e9f... --skill code-review

# Delete a profile
portico agents delete --tenant acme --id ap_3e9f...
```

`portico agents test` uses the same `Profile.Allows*` methods as the live dispatcher. The verdict it returns — `allowed: true` or `allowed: false` with a reason — matches what a live `tools/call` or `/v1/chat/completions` request would receive. This is the offline analogue of `kubectl auth can-i`.

---

## Profile resolution and caching

The resolver (`profiles.Resolver`) maps a `Principal{TenantID, Subject}` to a `*Profile`, with an in-memory LRU cache (default TTL: 60 seconds, capacity: 1024 entries). Resolution logic:

1. Check the in-memory cache. Return the cached Profile if present and not expired.
2. If the subject is empty, return the default Profile immediately (no store round-trip).
3. Call `AgentProfileStore.ResolveJWTBinding(ctx, tenantID, subject)`.
   - **Binding found**: return the stored Profile; cache it.
   - **`ErrAgentProfileNotFound`**: return the default Profile; cache it.
   - **Any other error**: return the error. The middleware fails the request with `503` rather than defaulting to the full surface (fail closed).

The resolver invalidates cache entries on Profile writes. Virtual Key resolution (attaching to a Profile via the VK's `profile_id`) follows the same path — the middleware resolves the bound Profile regardless of whether authentication came from a JWT or a VK.

::: tip Performance
The in-memory LRU makes the resolver overhead negligible on the hot path. The p95 microbenchmark gate (`BenchmarkResolver_HotPath`) enforces a ceiling of 1 ms with 1024 populated entries.
:::

---

## Multi-tenancy

Every Profile is keyed by `(tenant_id, id)`. Every read path in the storage layer filters on `tenant_id`. A JWT subject `alice@acme` under tenant `acme` is unrelated to the same string under tenant `beta` — the binding table primary key is `(tenant_id, jwt_sub)`. Cross-tenant reads are structurally impossible through the `AgentProfileStore` interface.

The synthesised default Profile carries the tenant's `TenantID` and is never mixed with another tenant's Profile in the resolver cache.

---

## Relationship to Virtual Keys and Budgets

Virtual Keys (see [Virtual Keys](/concepts/virtual-keys)) attach to Profiles via a `profile_id` foreign key. When a request authenticates via a Virtual Key, the middleware resolves the Profile bound to that VK. A VK may carry its own `allowed_mcp_servers` / `allowed_tools` slice; if present, the effective surface is the intersection with the bound Profile's allowlist (most-restrictive wins).

A single Profile may have multiple VKs — one per environment or lifecycle stage (development, staging, production). This keeps the logical agent definition stable while allowing credential rotation and per-environment budget tracking.

Budgets (see [Hierarchical Budgets](/concepts/hierarchical-budgets)) are attached to the VK, Team, or Customer level — not to the Profile directly. The Profile detail page in the Console aggregates spend across all VKs attached to that Profile.

---

## Relationship to A2A

When the A2A handler receives a task dispatch, it reads the calling principal's Profile from context and gates the outbound peer call through `Profile.AllowsA2APeer` and `Profile.AllowsA2ATask`. The same resolver, the same middleware step, and the same `profiles.FromContext(ctx)` call are used — there is no separate A2A entitlement surface.

Profiles also declare MCP↔A2A bridge routes (`mcp_to_a2a_bridges`, `a2a_to_mcp_bridges`) for cross-protocol dispatch. A bridge route is routing, not entitlement: the call still traverses the `Allows*` checks on the target side before dispatching. See [A2A Bridges](/concepts/a2a-bridges) for the full bridge semantics.

---

## Common pitfalls

**Profile vs. user.** A Profile is a *binding*, not an identity. One human operator may use multiple Profiles in different contexts (a restricted staging Profile and a full-surface debug Profile). A single Profile may serve many principals (every agent on the EU customer support team shares one Profile). The binding table maps subjects to Profiles many-to-one.

**Empty `allowed_tools` means all tools, not zero tools.** An empty list means "all tools in the allowed servers." To restrict to specific tools, list them explicitly. The Console wizard makes this explicit with a toggle ("Limit to specific tools?").

**Profile scopes narrow; they do not grant.** A Profile carrying `scopes: [mcp:call]` for a principal whose JWT carries `scopes: [mcp:call, llm:invoke]` results in effective scope `[mcp:call]`. A Profile cannot grant a scope the JWT did not carry.

**Stale sessions after a profile edit.** An active session retains its snapshot until the next refresh. Operators who need to immediately revoke access should use the explicit session-terminate action rather than editing the Profile and waiting for staleness to expire.

---

## Related

- [Virtual Keys](/concepts/virtual-keys) — credentials that attach to Profiles, one per environment or lifecycle stage
- [Hierarchical Budgets](/concepts/hierarchical-budgets) — spend limits tracked at the VK, Team, and Customer levels
- [Policy](/concepts/policy) — policy rules that reference Profile fields (`profile.id`, `profile.includes_server`) for fine-grained governance
- [Catalog and Sessions](/concepts/catalog-and-sessions) — how the per-session catalog snapshot is projected through the Profile's surface
- [A2A](/concepts/a2a) — agent-to-agent protocol and how Profile gates outbound peer dispatch
- [A2A Bridges](/concepts/a2a-bridges) — MCP↔A2A cross-protocol routing declared on a Profile
- [Create an Agent Profile](/guides/create-agent-profile) — step-by-step guide for the full operator workflow
- [Authentication](/concepts/authentication) — JWT validation and the principal identity that the resolver maps to a Profile
