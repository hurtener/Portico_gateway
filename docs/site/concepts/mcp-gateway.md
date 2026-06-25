# MCP Gateway overview

Portico speaks MCP outward — to AI clients, agents, and IDE integrations — and MCP inward — to the downstream servers that actually implement tools, resources, and prompts. This dual-facing design is the gateway's core value: a single, session-aware, tenant-isolated endpoint that a client connects to once and through which it reaches every server an operator has registered.

The four architectural layers underneath the gateway have their own deep pages:

- [Northbound](/concepts/mcp-northbound) — the HTTP+SSE transport a client connects to
- [Southbound](/concepts/mcp-southbound) — the stdio and HTTP clients Portico uses to reach downstream servers
- [Registry](/concepts/mcp-registry) — per-tenant, declarative server registration and lifecycle management
- [Catalog and sessions](/concepts/catalog-and-sessions) — snapshot-frozen session catalogs and drift detection

This page describes what the gateway does between them: protocol version pinning, capability negotiation, aggregation with namespacing, list-changed propagation, and the surface-projection hooks that restrict the catalog to what a specific consumer is entitled to see.

---

## Protocol version

Portico targets a single, pinned MCP protocol revision. The canonical constant lives at `internal/mcp/protocol/types.go`:

```go
const ProtocolVersion = "2025-11-25"
```

Bumping this value is an RFC change, not a code change. The `2025-11-25` revision introduced icon metadata on tools and servers, the `implementation.description` field, explicit Origin 403 enforcement, and clarifications to Streamable HTTP SSE resumption.

All wire types — request/response envelopes, capability blocks, tool and resource structs, notification payloads — are defined exclusively in `internal/mcp/protocol/`. No other package in the codebase defines MCP message structs. Adding a second definition site is a rejection-on-sight violation (see the contributor guidelines).

---

## Handshake and session lifecycle

Every interaction with Portico begins with the standard MCP `initialize` handshake. The northbound transport issues a fresh session ID on the `Mcp-Session-Id` response header during `initialize`; subsequent requests identify their session by sending that header. Requests without a session ID that are not `initialize` are rejected with a `session not found` error.

```http
POST /mcp HTTP/1.1
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
  "protocolVersion":"2025-11-25",
  "capabilities":{"elicitation":{}},
  "clientInfo":{"name":"my-agent","version":"1.0.0"}
}}
```

```http
HTTP/1.1 200 OK
Mcp-Session-Id: sess_7f3a...
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"result":{
  "protocolVersion":"2025-11-25",
  "capabilities":{...},
  "serverInfo":{"name":"portico","version":"0.x.y","description":"..."}
}}
```

The client then opens a long-lived SSE channel for server-to-client notifications:

```http
GET /mcp HTTP/1.1
Mcp-Session-Id: sess_7f3a...
Accept: text/event-stream
```

To terminate the session gracefully, send:

```http
DELETE /mcp HTTP/1.1
Mcp-Session-Id: sess_7f3a...
```

::: info Session isolation
Each session is bound to a tenant and a user, derived from the Bearer JWT that authenticates the `initialize` request. The session's effective catalog is snapshotted at creation time and stays stable for the session's lifetime unless the client explicitly opts in to live updates. See [Catalog and sessions](/concepts/catalog-and-sessions).
:::

---

## Capability negotiation and aggregation

After completing `initialize` handshakes with all active downstream servers, Portico aggregates their declared capabilities into a single `ServerCapabilities` block that it advertises to the northbound client. The aggregation rule is a union: a capability is present if at least one downstream advertises it.

From `internal/mcp/protocol/capabilities.go`:

```go
// AggregateServerCaps unions a set of downstream-server capabilities into
// the effective capability advertised by Portico. A capability is present
// iff at least one downstream advertises it.
func AggregateServerCaps(downstream []ServerCapabilities) ServerCapabilities
```

The aggregated capability block covers:

| Capability | Union field | Meaning |
|---|---|---|
| `tools.listChanged` | OR across downstreams | Portico will forward `notifications/tools/list_changed` |
| `resources.subscribe` | OR across downstreams | At least one server supports `resources/subscribe` |
| `resources.listChanged` | OR across downstreams | Portico will forward `notifications/resources/list_changed` |
| `prompts.listChanged` | OR across downstreams | Portico will forward `notifications/prompts/list_changed` |
| `logging` | OR across downstreams | At least one server supports logging |

Portico-internal capabilities (for example, the gateway's own logging surface) are layered on top of the aggregated block after this step.

The client's own capability declaration is captured separately. Whether the client advertised `elicitation` determines whether Portico uses `elicitation/create` or falls back to a structured error code when a tool call requires approval. See [Approvals](/concepts/approvals) for the full flow.

---

## Aggregation with namespacing

Downstream servers routinely expose tools, resources, and prompts with identical names. Portico rewrites every identifier before surfacing it northbound, using the server's registered ID as a namespace prefix. This makes routing deterministic and eliminates collisions.

### Tool namespacing

Tools use a `{server_id}.{tool_name}` convention. The `namespace` package implements this:

```go
// JoinTool produces the on-the-wire tool name.
func JoinTool(serverID, toolName string) string { return serverID + "." + toolName }

// SplitTool inverts JoinTool: extracts (serverID, toolName) from the first dot.
func SplitTool(qualified string) (serverID, toolName string, ok bool)
```

Tool names that already contain dots are preserved: split operates on the **first** dot only, so `github.get_pull_request` routes to the `github` server's `get_pull_request` tool, and a tool published as `github.some.sub.thing` routes to `github`'s `some.sub.thing`.

Server IDs are validated at registration time against `^[a-z0-9][a-z0-9_-]{0,31}$`, which prevents collisions with the separator and ensures uniqueness of the routing key.

A client therefore calls `github.get_pull_request` — never `get_pull_request` directly. The dispatcher splits the qualified name, locates the right southbound client, strips the prefix, and forwards the bare `get_pull_request` to the downstream.

### Resource URI namespacing

Resource URIs require more nuance than tool names because they carry scheme and authority. Portico rewrites them into a namespaced form before surfacing them northbound, and restores them before forwarding to the downstream.

| Downstream URI scheme | Northbound form |
|---|---|
| `ui://...` | `ui://{server_id}/{rest}` |
| `file://...` | `mcp+server://{server_id}/file/{path}` |
| `https://host/path` | `mcp+server://{server_id}/https/{authority}/{path}` |
| `http://host/path` | `mcp+server://{server_id}/http/{authority}/{path}` |
| Any other scheme | `mcp+server://{server_id}/raw/{base64url(original)}` |

Rewrites are idempotent: an already-namespaced URI is returned unchanged. The original URI is preserved in the resource's `_meta.upstreamURI` field for transparency.

`ui://` URIs are treated specially: they identify MCP App resources that are subject to CSP wrapping at the gateway boundary. See [MCP Northbound](/concepts/mcp-northbound) for Origin enforcement and [Skill Packs](/concepts/skill-packs) for how Skills attach UI resources.

### Prompt namespacing

Prompts follow the same `{server_id}.{prompt_name}` pattern as tools, with the same first-dot split semantics:

```go
func RewritePromptName(serverID, original string) string
func RestorePromptName(qualified string) (serverID, original string, ok bool)
```

---

## List-changed propagation

When a downstream server emits `notifications/tools/list_changed`, `notifications/resources/list_changed`, or `notifications/prompts/list_changed`, Portico forwards those notifications to any northbound session that has the corresponding downstream server in its effective catalog.

Sessions that have the gateway's list-changed capability bit set receive the notification on their SSE channel. Sessions in snapshot-stable mode (the default) additionally trigger an internal diff comparison: the live tool schema is re-fetched, fingerprinted, and compared against the snapshot. If the schemas differ, a `schema.drift` audit event is emitted. The session's tool list itself does not silently change — the client must re-issue `tools/list` to refresh, or the operator must configure live-update mode.

This architecture means that a downstream upgrade that adds or removes tools is never invisible: it either appears as a changed response to `tools/list` (live-update mode) or as an auditable drift event (snapshot mode).

---

## Server-initiated requests (elicitation)

MCP `2025-11-25` introduced a server-initiated request channel: Portico can send JSON-RPC requests **to** the client over the SSE channel and wait for responses. The primary use is `elicitation/create`, which Portico dispatches when a tool call triggers a policy that requires human approval and the client advertised elicitation capability during the handshake.

The implementation serializes the request as `event: server_request` on the SSE stream. The client sends its response as a standard JSON-RPC reply in a subsequent POST. The northbound transport's `TryDeliver` path matches the response ID against a pending entry and routes it to the waiting goroutine, short-circuiting normal dispatch:

```
Server                             Client
  │── event: server_request ──────>│  (SSE)
  │<── POST /mcp (response) ───────│
```

---

## Error codes

All error codes are defined in `internal/mcp/protocol/errors.go`. The standard JSON-RPC codes (`-32700` through `-32603`) follow the specification. Portico-defined codes occupy the implementation-reserved range `-32000` to `-32099`:

| Code | Constant | Meaning |
|---|---|---|
| `-32001` | `ErrApprovalRequired` | Tool call blocked pending human approval |
| `-32002` | `ErrUpstreamUnavailable` | Downstream transport error (server unreachable or crashed) |
| `-32003` | `ErrPolicyDenied` | Tool call denied by policy engine |
| `-32004` | `ErrToolNotEnabled` | Tool not in the session's effective catalog or namespace mismatch |
| `-32005` | `ErrTenantInactive` | Tenant is disabled |
| `-32006` | `ErrAgentProfileViolation` | Tool, alias, or skill outside the bound Agent Profile's surface |
| `-32007` | `ErrVKScopeViolation` | Server or tool outside the Virtual Key's allowlist |

Code Mode errors use a separate range (`-32010` to `-32012`), with a structured `data.code` field carrying the specific reason string (`code_mode.snapshot_drifted`, `code_mode.budget_exceeded`, etc.).

::: tip Aggregator behavior on method-not-found
When the gateway forwards a request to a downstream that does not implement a given surface (for example, a `resources/list` call against a tools-only server), the downstream returns `-32601` (method not found). The aggregator detects this with `IsMethodNotFound` and silently skips that server rather than treating the response as a partial failure. Clients see a clean merged result, not an error.
:::

---

## Surface projection through Agent Profiles

The raw aggregated catalog — every tool from every server — is rarely what a given consumer should see. [Agent Profiles](/concepts/agent-profiles) are the mechanism that narrows this surface to a specific consumer's entitlement.

An Agent Profile binds a principal (a JWT subject or a Virtual Key) to a subset of servers, an optional finer-grain tool allowlist, a subset of Skill Packs, and a set of LLM model aliases. The Profile resolver runs once per request, after authentication and before dispatch, and writes the resolved Profile into the request context.

Every downstream gate — `tools/list`, `tools/call`, `prompts/list`, `resources/list`, the Skill surface — reads the Profile via a single helper and calls its `AllowsTool`, `AllowsServer`, `AllowsSkill`, and `AllowsAlias` decision methods. There is no parallel allowlist on any surface; the Profile is the single source of truth.

A tool that exists in the aggregated catalog but falls outside the bound Profile's surface is invisible at `tools/list` and returns `ErrAgentProfileViolation` (`-32006`) at `tools/call`. From the client's perspective, the tool does not exist.

::: info Back-compat by construction
A principal with no Profile bound resolves to a synthesized default Profile that allows the tenant's full surface. A deployment that configures no Profiles behaves exactly as a deployment without Agent Profiles at all — there is no opt-in flag and no migration required.
:::

---

## Code Mode: an alternate catalog projection

[Code Mode](/concepts/code-mode) is a per-session, opt-in projection of the same governed catalog that replaces the standard N-tool list with four meta-tools. Clients opt in by including `capabilities.experimental.portico.code_mode` during `initialize`.

When Code Mode is active, `tools/list` returns exactly four tools under the reserved `mcp.*` namespace:

| Tool | Purpose |
|---|---|
| `mcp.listToolFiles` | Enumerate virtual `.pyi` stub files for the session's snapshot |
| `mcp.readToolFile` | Read one stub file |
| `mcp.getToolDocs` | Full schema and policy metadata for named tools |
| `mcp.executeToolCode` | Run a Starlark snippet that calls tools through governed bindings |

This projection avoids sending a large tool schema payload to the model on every request. In-sandbox tool calls traverse the identical tenant, policy, approval, credential, and audit envelope as direct `tools/call` requests — Code Mode is a presentation layer over the same governance, not a bypass.

The `mcp.*` namespace is reserved. No tool from any downstream server can be routed under it, and no code outside `internal/mcp/codemode/` may register names in that namespace.

See [Code Mode](/concepts/code-mode) for sandbox guarantees, budget controls, and the approval-suspension/continuation protocol.

---

## How the layers compose

```
AI Client / Agent
     │
     │  HTTP+SSE (MCP/2025-11-25, Bearer JWT)
     ▼
┌────────────────────────────────────────────────┐
│  Northbound transport  (POST/GET/DELETE /mcp)  │
│  • Origin guard                                │
│  • Session creation / Mcp-Session-Id           │
│  • SSE notification channel                    │
│  • Server-initiated request (elicitation)      │
└──────────────────┬─────────────────────────────┘
                   │
┌──────────────────▼─────────────────────────────┐
│  Dispatcher                                    │
│  • Auth + tenant resolution                    │
│  • Agent Profile surface projection            │
│  • Aggregation + namespace rewriting           │
│  • Policy engine + approval flow               │
│  • Credential injection                        │
│  • Catalog snapshot + drift detection          │
│  • Audit emit                                  │
└──────┬─────────────────────────┬───────────────┘
       │                         │
  stdio clients            HTTP clients
  (per-tenant/user/        (remote_static,
   session processes)       shared)
       │                         │
   GitHub MCP           Postgres MCP   Linear MCP ...
```

Each sub-layer has one responsibility and one interface. The southbound `Client` interface (`internal/mcp/southbound/types.go`) is the seam between the dispatcher and transport: adding a new transport — for example, WebSocket — requires extending that interface in one place without touching dispatch logic.

---

## Related

- [MCP Northbound](/concepts/mcp-northbound) — transport, Origin enforcement, SSE event format, session headers
- [MCP Southbound](/concepts/mcp-southbound) — stdio and HTTP client implementations, the `Client` interface
- [MCP Registry](/concepts/mcp-registry) — server registration, runtime modes, lifecycle and health
- [Catalog and sessions](/concepts/catalog-and-sessions) — snapshot freezing, drift detection, live-update mode
- [Agent Profiles](/concepts/agent-profiles) — consumer entitlement, surface projection, intersection semantics
- [Code Mode](/concepts/code-mode) — Starlark sandbox, meta-tools, token-savings projection
- [Approvals](/concepts/approvals) — elicitation flow, structured error fallback, approval replay
- [Architecture](/concepts/architecture) — how the gateway fits into Portico's full system diagram
