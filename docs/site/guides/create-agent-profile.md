# Create an Agent Profile

An Agent Profile is Portico's single source of truth for what a logical agent
is allowed to do: which MCP servers and individual tools it may reach, which
Skill Packs it may invoke, which LLM model aliases it may request, and which
scopes it carries. Every downstream gating surface — the MCP dispatcher, the
LLM gateway, the Skills runtime — reads the resolved Profile from the request
context. There is no parallel allowlist on any other surface.

This guide walks through the full lifecycle: defining a Profile in configuration
or at runtime, binding it to a principal, verifying the projected surface, and
understanding the enforcement signals you will see when a request falls outside
it.

## Background

Before a request reaches any tool or model, Portico resolves the caller's
identity (JWT subject or Virtual Key) to a Profile. A caller with no Profile
binding receives the **default profile** — the tenant's full surface — which
preserves backward compatibility with deployments that predate Profile-aware
configuration. Operators opt into restriction by creating a Profile and binding
their principal to it; restriction is never applied implicitly.

Key design rules that affect everything below:

- **Intersection semantics.** When a Virtual Key also carries its own MCP
  allowlist, the effective surface is the intersection of both: the most
  restrictive layer wins. A Profile can only narrow; it never grants access
  beyond what the tenant's VK or scope layers allow.
- **The default profile is never stored.** `GET /api/agent-profiles` lists only
  explicitly created profiles. The synthesised default is a code construct; it
  has no row in the database and will not appear in the list response.
- **Cache TTL of 60 seconds.** The resolver caches bindings in memory with a
  60-second TTL. After a CRUD write, the gateway invalidates the affected
  tenant's cache entries immediately, so changes take effect without waiting for
  expiry.

See [Agent Profiles](/concepts/agent-profiles) for a full conceptual treatment.

---

## Step 1: Define the Profile

There are three ways to define a Profile:

1. **Static YAML** in `portico.yaml` (declarative, cold-start seeded).
2. **REST API** (`POST /api/agent-profiles`).
3. **CLI** (`portico agents create`).

All three paths share the same schema. Start with YAML to understand the shape,
then use the API or CLI for runtime management.

### Option A — Static YAML

Add an `agent_profiles:` block to `portico.yaml`. Profiles are seeded into the
tenant's store at boot; the operation is idempotent (matched by `tenant + name`,
updated in place on restart).

```yaml
agent_profiles:
  - name: "customer-support"
    description: "Read-only ticket access. GDPR-compliant model set."
    allowed_mcp_servers:
      - zendesk
      - intercom
    allowed_tools:
      - zendesk.search_tickets
      - zendesk.get_ticket
      - intercom.list_conversations
    allowed_skills:
      - customer-support-triage
    allowed_model_aliases:
      - fast-summary
    scopes:
      - mcp:call
      - llm:invoke
    bindings:
      - "agent:customer-support@acme.example"

  - name: "engineering-debug"
    description: "Full engineering surface — scoped via JWT sub."
    allowed_mcp_servers:
      - github
      - jira
      - sentry
    allowed_skills:
      - code-review
      - incident-triage
    allowed_model_aliases:
      - gpt-4o
      - claude-3-5-sonnet
    scopes:
      - mcp:call
      - llm:invoke
```

**`allowed_tools` is optional.** When absent, the profile permits all tools
exposed by the listed servers. When present, only those namespaced tool IDs
(`server.tool`) are projected into `tools/list`.

The `tenant` field is required when more than one tenant is configured. When
exactly one tenant is configured it defaults to that tenant.

A `portico.yaml` without `agent_profiles:` means no profiles are configured —
every authenticated request receives the default profile (V1/V1.5 behaviour
unchanged).

### Option B — REST API

The REST surface requires the `admin` scope on the caller's JWT or Virtual Key.

```http
POST /api/agent-profiles
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "name": "customer-support",
  "description": "Read-only ticket access. GDPR-compliant model set.",
  "allowed_mcp_servers": ["zendesk", "intercom"],
  "allowed_tools": [
    "zendesk.search_tickets",
    "zendesk.get_ticket",
    "intercom.list_conversations"
  ],
  "allowed_skills": ["customer-support-triage"],
  "allowed_model_aliases": ["fast-summary"],
  "scopes": ["mcp:call", "llm:invoke"],
  "enabled": true
}
```

The server generates the profile ID (`ap_<random-hex>`); any `id` field in the
request body is ignored. The response is `201 Created` with the full profile
object including the generated `id`, `created_at`, and `updated_at`.

To update an existing profile use `PUT /api/agent-profiles/{id}` with the same
body shape. The URL `id` is authoritative; a body `id` is ignored.

**JSON fields reference:**

| Field | Type | Notes |
|---|---|---|
| `name` | string | Required. Must be unique within the tenant. |
| `description` | string | Optional. |
| `allowed_mcp_servers` | `[]string` | Server names from the tenant's registry. Empty = no servers (fully restricted). |
| `allowed_tools` | `[]string` | Namespaced `server.tool` IDs. Empty = all tools in `allowed_mcp_servers`. |
| `allowed_skills` | `[]string` | Skill Pack IDs. Empty = no skills. |
| `allowed_model_aliases` | `[]string` | LLM model aliases. Empty = no aliases. |
| `allowed_a2a_peers` | `[]string` | A2A peer names. Empty = no A2A peers. |
| `allowed_a2a_tasks` | `[]string` | Namespaced `peer.task` IDs. Empty = all tasks of allowed peers. |
| `scopes` | `[]string` | Effective scope set this profile carries. |
| `policy_bundle_ref` | string | Optional reference to a policy bundle. |
| `enabled` | bool | Defaults to `true`. |

### Option C — CLI

The `portico agents` subcommand operates directly against the SQLite store (no
running gateway required). Flags use comma-separated lists for multi-value
fields.

```bash
# Create a profile
portico agents create \
  --tenant acme \
  --name customer-support \
  --servers zendesk,intercom \
  --tools "zendesk.search_tickets,zendesk.get_ticket,intercom.list_conversations" \
  --skills customer-support-triage \
  --models fast-summary \
  --scopes "mcp:call,llm:invoke" \
  --description "Read-only ticket access"

# List profiles for a tenant
portico agents list --tenant acme

# Fetch a single profile by ID
portico agents get --tenant acme --id ap_<hex>

# Delete a profile
portico agents delete --tenant acme --id ap_<hex>
```

The `--dsn` flag defaults to `file:./data/portico.db`; pass a different path if
your database is elsewhere.

---

## Step 2: Bind a Principal to the Profile

A Profile has no effect until at least one principal (JWT subject) is bound to
it. Bindings are the mapping `jwt_sub → profile_id`; a subject has at most one
binding at a time.

### Via REST

```http
PUT /api/agent-profiles/{id}/bindings/{sub}
Authorization: Bearer <admin-jwt>
```

Returns `204 No Content` on success. The binding takes effect immediately (the
resolver cache is invalidated for the tenant).

To remove a binding:

```http
DELETE /api/agent-profiles/{id}/bindings/{sub}
Authorization: Bearer <admin-jwt>
```

After unbinding, requests from that subject fall back to the default profile
(full tenant surface).

### Via CLI

```bash
# Bind a JWT subject
portico agents bind \
  --tenant acme \
  --id ap_<hex> \
  --sub "agent:customer-support@acme.example"

# Remove a binding
portico agents unbind \
  --tenant acme \
  --sub "agent:customer-support@acme.example"
```

### Attaching Virtual Keys

A [Virtual Key](/concepts/virtual-keys) can be attached to a Profile by
supplying `profile_id` when creating the key:

```http
POST /api/governance/virtual-keys
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "name": "cs-agent-staging",
  "profile_id": "ap_<hex>",
  "scopes": ["mcp:call", "llm:invoke"]
}
```

Requests authenticated by this Virtual Key resolve to the attached Profile. The
effective surface is the intersection of the VK's own allowlists (if any) and
the Profile's allowlists — whichever is more restrictive governs.

---

## Step 3: Verify the Projected Surface

After creating a Profile and binding at least one principal, verify what the
Profile actually sees given the current live state of the tenant's catalog.

### Live surface endpoint

```http
GET /api/agent-profiles/{id}/surface
Authorization: Bearer <admin-jwt>
```

Returns a `Surface` object:

```json
{
  "profile_id": "ap_3f2a...",
  "is_default": false,
  "servers": ["zendesk", "intercom"],
  "tools": [
    "zendesk.search_tickets",
    "zendesk.get_ticket",
    "intercom.list_conversations"
  ],
  "skills": ["customer-support-triage"],
  "models": ["fast-summary"]
}
```

The surface is the intersection of the Profile's declared allowlists with the
**live catalog** at the moment of the request. A server registered after the
Profile was created but matching its `allowed_mcp_servers` list appears here
immediately, without restarting or updating the Profile.

### CLI dry-run test

Use `portico agents test` to ask "would this profile allow this target?" offline.
The command uses the same `Profile.Allows*` decision methods the live dispatcher
routes through, so the verdict matches production exactly:

```bash
# Test a namespaced tool
portico agents test \
  --tenant acme \
  --id ap_<hex> \
  --tool zendesk.search_tickets

# Test an LLM model alias
portico agents test \
  --tenant acme \
  --id ap_<hex> \
  --alias fast-summary

# Test a Skill Pack
portico agents test \
  --tenant acme \
  --id ap_<hex> \
  --skill customer-support-triage
```

Example response:

```json
{
  "profile_id": "ap_3f2a...",
  "tenant": "acme",
  "kind": "tool",
  "target": "zendesk.search_tickets",
  "allowed": true,
  "reason": "tool_in_profile"
}
```

Pass exactly one of `--tool`, `--alias`, or `--skill`; the command errors if
more than one is supplied.

---

## Step 4: Observe Enforcement

Once a Profile is in effect, Portico enforces it at multiple gating surfaces.

### MCP `tools/list` filtering

An MCP session authenticated by a principal bound to the `customer-support`
Profile will receive only the tools in that Profile's surface from `tools/list`.
Tools from servers outside `allowed_mcp_servers` are absent from the JSON array
— not just hidden, but not serialised at all.

### MCP `tools/call` rejection

A `tools/call` for a tool outside the Profile's surface returns a typed JSON-RPC
error with code `-32006`:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32006,
    "message": "agent profile violation",
    "data": {
      "profile_id": "ap_3f2a...",
      "tool": "github.create_pr",
      "reason": "tool_outside_profile"
    }
  }
}
```

An `agent_profile.violation` audit event is emitted simultaneously, recording
`profile_id`, `tool`, and `reason`.

### LLM model alias rejection

A request to `/v1/chat/completions` with a `model` alias outside
`allowed_model_aliases` returns:

```http
HTTP/1.1 403 Forbidden
Content-Type: application/json

{
  "error": "agent_profile_violation",
  "message": "model alias is outside the agent profile surface: gpt-4o",
  "details": {
    "profile_id": "ap_3f2a...",
    "alias": "gpt-4o"
  }
}
```

`GET /v1/models` is also filtered: only the aliases in `allowed_model_aliases`
appear in the response.

### Intersection with Virtual Key allowlists

If the authenticating Virtual Key also declares `allowed_mcp_servers`, the
effective surface is the **intersection** of both:

```
effective_servers = profile.allowed_mcp_servers
                  ∩ vk.allowed_mcp_servers   (if the VK has its own list)
```

The most restrictive layer always wins. Neither layer can grant access the other
disallows.

---

## Step 5: Manage the Profile Lifecycle

### Update

`PUT /api/agent-profiles/{id}` replaces the entire profile (including all
allowlists) atomically. The store deletes the existing allowlist join rows and
inserts the new ones in a single transaction; `created_at` is preserved and
`updated_at` is set to the current time.

```http
PUT /api/agent-profiles/ap_3f2a...
Authorization: Bearer <admin-jwt>
Content-Type: application/json

{
  "name": "customer-support",
  "allowed_mcp_servers": ["zendesk", "intercom", "freshdesk"],
  "allowed_tools": [],
  "allowed_skills": ["customer-support-triage"],
  "allowed_model_aliases": ["fast-summary", "standard-summary"],
  "scopes": ["mcp:call", "llm:invoke"],
  "enabled": true
}
```

Setting `allowed_tools: []` (empty) means "all tools in the allowed servers" —
the finer-grain per-tool allowlist is cleared.

The resolver cache is invalidated immediately on a successful update.

### Delete

```http
DELETE /api/agent-profiles/ap_3f2a...
Authorization: Bearer <admin-jwt>
```

Returns `204 No Content`. The join tables cascade: all allowlist rows and all
JWT bindings referencing this profile are removed atomically. Subjects that were
bound to the deleted Profile fall back to the default profile.

### List

```http
GET /api/agent-profiles
Authorization: Bearer <admin-jwt>
```

Returns the tenant's stored profiles sorted by name. **The default profile is
never returned here** — it is a code construct, not a stored row.

---

## Troubleshooting

**`tools/list` still shows tools from a disallowed server after binding.**

The resolver has a 60-second cache TTL; the gateway invalidates on write but
if you are testing against a different instance, wait up to 60 seconds or
restart the gateway in dev mode.

**`portico agents test` says `allowed: true` but `tools/call` returns
`agent_profile_violation`.**

Check that the `--tenant` and `--id` flags match the tenant and profile the
live request is resolving to. Use `GET /api/agent-profiles/{id}` to confirm
the stored allowlists are what you expect, and `GET
/api/agent-profiles/{id}/surface` to see the live intersection.

**`GET /api/agent-profiles` returns an empty array.**

No profiles have been created for this tenant. Requests are receiving the
default profile (full surface). Create a profile and bind a principal to opt
into restriction.

**A Virtual Key request ignores the profile.**

Virtual Key profile linkage uses the `profile_id` field set at VK creation
time. Existing VKs created before Profile support was deployed have no
`profile_id`. Re-create or rotate the VK and supply the `profile_id` in the
request body.

---

## Related

- [Agent Profiles concept](/concepts/agent-profiles) — architecture, intersection semantics, resolver design.
- [Virtual Keys](/concepts/virtual-keys) — attaching profiles to programmatic credentials.
- [Policy](/concepts/policy) — how profile-level policy bundle references work alongside the core policy engine.
- [Register an MCP Server](/guides/register-mcp-server) — ensure the servers you reference in `allowed_mcp_servers` are registered before binding principals.
- [REST API reference](/reference/rest-api) — full endpoint index including all `/api/agent-profiles` routes.
- [CLI reference](/reference/cli) — complete flag listing for `portico agents`.
