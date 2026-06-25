# Set up an A2A peer

This guide walks through the complete lifecycle of connecting Portico to an external A2A agent as a
governed peer: registering the peer, ingesting its agent card, wiring egress credentials from the
vault, declaring bridge routes on an Agent Profile, and dispatching tasks — with every step verified
against the audit trail.

::: info Prerequisites
You need a running Portico instance with admin access, the `PORTICO_VAULT_KEY` environment variable
set (base64-encoded 32-byte key), and an external agent that exposes a standard A2A endpoint and
publishes an agent card at `/.well-known/agent.json`.
:::

## How A2A peering works

A registered peer is a tenant-scoped record that tells Portico where an external agent lives, how to
authenticate outbound calls to it, and what it can do. The governing envelope is the same one every
other protocol traverses on Portico:

```
inbound caller
   → tenant identity (JWT or Virtual Key)
   → Agent Profile entitlement (AllowsA2APeer / AllowsA2ATask)
   → southbound pool (per-(tenant, peer) client, vault-resolved credentials)
   → peer's JSON-RPC endpoint
   → audit event (a2a.dispatch or agent_profile.violation)
```

Portico is a governed single-endpoint A2A proxy: the caller names the target peer in
`params.metadata.portico_peer`; Portico enforces the profile, attaches egress auth, dispatches, and
records the result. Credentials never flow in the other direction — the caller's `Authorization`
header is never forwarded to the peer.

See [A2A overview](/concepts/a2a) for protocol background, and [A2A bridges](/concepts/a2a-bridges)
for the cross-protocol routing model.

---

## Step 1 — Register the peer

Create the peer record. The `name` field is how bridges and Agent Profile allowlists refer to the
peer; keep it stable.

```http
POST /api/a2a/peers
Authorization: Bearer <admin-token>
Content-Type: application/json

{
  "name": "research-agent",
  "endpoint": "https://research.example.internal/a2a",
  "egress_auth_ref": "research-agent-bearer",
  "enabled": true
}
```

**Response (201):**

```json
{
  "id": "a2a_3f8c9d1a2b4e5f60",
  "name": "research-agent",
  "endpoint": "https://research.example.internal/a2a",
  "egress_auth_ref": "research-agent-bearer",
  "agent_card_json": "",
  "enabled": true,
  "created_at": "2026-06-24T10:00:00Z",
  "updated_at": "2026-06-24T10:00:00Z"
}
```

The `id` is server-generated (`a2a_` + 16 random hex bytes). Save it — you'll use it for card
refresh and later updates. The `agent_card_json` field is empty until the first card refresh.

Both `name` and `endpoint` are required. `egress_auth_ref` is the vault key whose value Portico
resolves to a `Bearer` token for outbound requests; leave it empty only if the peer requires no
authentication.

::: warning Admin scope required
All A2A peer CRUD endpoints require the `admin` scope on the caller's JWT or Virtual Key.
:::

---

## Step 2 — Store egress credentials in the vault

Before the first dispatch, write the peer's API token to the vault. The `egress_auth_ref` value you
set on the peer (`"research-agent-bearer"` above) is the secret name Portico will look up.

```bash
./bin/portico vault put \
  --tenant acme \
  --name research-agent-bearer \
  --value "Bearer t-live-…"
```

The vault stores only `salt` + `HMAC-SHA256(salt, secret)` at rest; the plaintext is never
persisted after write. On every outbound request the southbound client calls the vault, receives the
plaintext in memory, and sets `Authorization: <resolved value>` on the HTTP request to the peer.

See [Credentials vault](/concepts/credentials-vault) for key rotation, the `PORTICO_VAULT_KEY`
requirement, and the injector model.

---

## Step 3 — Ingest the agent card (refresh-card)

The agent card is the A2A discovery document. Portico fetches it from the peer's well-known URL
(`<peer-endpoint-scheme>://<host>/.well-known/agent.json`), caches the JSON on the peer row, and
surfaces the discovered skills in Portico's own agent card.

```http
POST /api/a2a/peers/a2a_3f8c9d1a2b4e5f60/refresh-card
Authorization: Bearer <admin-token>
```

**Response (200):**

```json
{
  "id": "a2a_3f8c9d1a2b4e5f60",
  "name": "research-agent",
  "endpoint": "https://research.example.internal/a2a",
  "egress_auth_ref": "research-agent-bearer",
  "agent_card_json": "{\"name\":\"Research Agent\",\"description\":\"...\",\"url\":\"https://research.example.internal/a2a\",\"version\":\"1.0.0\",\"protocolVersion\":\"0.2.5\",\"capabilities\":{},\"skills\":[{\"id\":\"code-review\",\"name\":\"Code Review\",\"description\":\"Reviews code diffs and returns structured feedback.\"}]}",
  "enabled": true,
  "created_at": "2026-06-24T10:00:00Z",
  "updated_at": "2026-06-24T10:01:00Z"
}
```

The refresh uses the peer's configured egress credentials, so the vault secret must be in place
first (Step 2). A card fetch failure returns `502` with error code `a2a_card_fetch_failed`.

After a successful refresh the peer's skills appear in `GET /a2a/.well-known/agent.json` under the
tenant's aggregated card. Refresh again at any time to pick up updates to the peer's skill set.

---

## Step 4 — Declare bridges and allowlists on an Agent Profile

An Agent Profile controls which peers a consumer can reach and optionally routes named MCP tools
transparently to A2A tasks. The profile is the single source of entitlement — there is no
alternative allowlist path.

```http
PUT /api/agent-profiles/prof_abc123
Authorization: Bearer <admin-token>
Content-Type: application/json

{
  "name": "ci-agent-profile",
  "allowed_a2a_peers": ["research-agent"],
  "allowed_a2a_tasks": ["research-agent.code-review"],
  "mcp_to_a2a_bridges": [
    {
      "mcp_tool": "github.review.run",
      "a2a_peer": "research-agent",
      "a2a_task": "code-review"
    }
  ],
  "allowed_mcp_servers": ["github"],
  "allowed_tools": ["github.review.run"],
  "enabled": true
}
```

The key fields for A2A:

| Field | Semantics |
|---|---|
| `allowed_a2a_peers` | Peer names the consumer may dispatch to. Empty = all registered peers. |
| `allowed_a2a_tasks` | Namespaced tasks (`"peer.task"`) the consumer may invoke. Empty = all tasks of the allowed peers. |
| `mcp_to_a2a_bridges` | Routing table: an MCP `tools/call` for `mcp_tool` dispatches to `a2a_peer`'s `a2a_task` transparently. |
| `a2a_to_mcp_bridges` | Inverse: an inbound A2A task named `a2a_task` dispatches to the MCP tool `mcp_tool`. |

Bridges are routing, not entitlement: a bridged call is still gated by `AllowsA2APeer` and
`AllowsA2ATask` before it leaves the gateway.

::: tip Empty allowlists mean "all"
A consumer with `allowed_a2a_peers: []` can reach every enabled peer in the tenant. Restrict to
specific peers as soon as you know which ones an agent legitimately needs.
:::

Bind the profile to a JWT subject so the consumer's token resolves to it on every request:

```http
POST /api/agent-profiles/prof_abc123/jwt-bindings
Authorization: Bearer <admin-token>
Content-Type: application/json

{ "jwt_sub": "ci-pipeline@tenant.example" }
```

See [Agent Profiles](/concepts/agent-profiles) and [Create an Agent Profile](/guides/create-agent-profile)
for the full profile schema.

---

## Step 5 — Dispatch a task via POST /a2a

With the peer registered, the card ingested, egress credentials loaded, and the profile bound, the
consumer can dispatch tasks. The endpoint is `POST /a2a`; it accepts a standard A2A JSON-RPC 2.0
body. The only Portico-specific addition is `params.metadata.portico_peer`, which names the
registered peer by its stored `id` (not its `name`).

```http
POST /a2a
Authorization: Bearer <consumer-token>
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "messageId": "msg-001",
      "kind": "message",
      "parts": [
        {
          "kind": "data",
          "data": {
            "pr_url": "https://github.com/example/repo/pull/42",
            "focus": "security"
          }
        }
      ]
    },
    "metadata": {
      "portico_peer": "a2a_3f8c9d1a2b4e5f60"
    }
  }
}
```

::: warning Use the peer id, not the peer name
`params.metadata.portico_peer` takes the opaque id returned at create time (`a2a_…`), not the
human-readable `name`. The name is used in profile allowlists and bridge declarations; the id is the
routing key on the wire.
:::

**Successful response (200):**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "id": "task-99f1",
    "kind": "task",
    "status": {
      "state": "completed",
      "timestamp": "2026-06-24T10:05:00Z"
    },
    "artifacts": [
      {
        "artifactId": "artifact-001",
        "name": "review",
        "parts": [
          { "kind": "text", "text": "No high-severity issues found. One medium: …" }
        ]
      }
    ]
  }
}
```

The result is whatever the peer returned — a `Task` or a `Message` in its terminal state. Portico
does not inspect or transform the body beyond JSON-RPC envelope handling.

### Checking task status after a non-blocking dispatch

If the peer returned `status.state: "working"`, the task is still running. Poll with `tasks/get`:

```http
POST /a2a
Authorization: Bearer <consumer-token>
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tasks/get",
  "params": {
    "id": "task-99f1",
    "metadata": {
      "portico_peer": "a2a_3f8c9d1a2b4e5f60"
    }
  }
}
```

Cancel with `tasks/cancel` using the same shape.

---

## Transparent bridging from MCP

When the consumer uses MCP and the Agent Profile declares an `mcp_to_a2a_bridges` entry, a
`tools/call` for the named tool is silently re-routed to the A2A peer — the consumer never knows it
happened.

With the profile from Step 4, a `tools/call` for `github.review.run`:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "github.review.run",
    "arguments": {
      "pr_url": "https://github.com/example/repo/pull/42",
      "focus": "security"
    }
  }
}
```

...translates internally to a `message/send` to `research-agent` with `a2a_task: "code-review"` in
the params metadata. The MCP arguments are mapped to an A2A `DataPart` (for object arguments) or a
`TextPart` (for non-object payloads). The A2A result is returned as an MCP `CallToolResult` carrying
the raw JSON in both a `text` content block and `structuredContent`.

The bridge still traverses the full governance envelope: `AllowsA2APeer("research-agent")` and
`AllowsA2ATask("research-agent.code-review")` are evaluated before any outbound call is made.

---

## Verifying gating: AllowsA2APeer and the audit trail

### Profile enforcement

The `AllowsA2APeer` check runs in the `dispatch` package before any southbound call. A consumer
whose profile does not list the peer receives a JSON-RPC error with Portico-defined code `-32010`
(`ErrProfileViolation`):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32010,
    "message": "agent profile violation",
    "data": {
      "profile_id": "prof_abc123",
      "peer": "research-agent",
      "reason": "peer_outside_profile"
    }
  }
}
```

For bridged tool calls the error surfaces as an MCP `ErrAgentProfileViolation` with the same
structured data in the error's `data` field.

### Audit events

Every governed dispatch emits an `a2a.dispatch` event. Profile rejections emit
`agent_profile.violation`. Both carry `tenant_id` and are queryable through the audit API.

```http
GET /api/audit/events?type=a2a.dispatch&limit=20
Authorization: Bearer <admin-token>
```

Example `a2a.dispatch` payload:

```json
{
  "peer_id": "a2a_3f8c9d1a2b4e5f60",
  "peer": "research-agent",
  "method": "message/send"
}
```

Example `agent_profile.violation` payload (peer rejected):

```json
{
  "profile_id": "prof_abc123",
  "peer": "research-agent",
  "reason": "peer_outside_profile"
}
```

For bridged MCP calls the audit trail contains two events: an `a2a.dispatch` emitted by the A2A
dispatcher, and a second bridge-side event emitted by the MCP dispatcher carrying `mcp_tool`,
`a2a_peer`, `a2a_task`, and `duration_ms`.

See [Audit](/concepts/audit) and [Observability](/concepts/observability) for querying and retention
configuration.

---

## Managing peer lifecycle

### Updating a peer

`PUT /api/a2a/peers/{id}` accepts the same fields as create. If `name` or `endpoint` are omitted
they are left unchanged; `egress_auth_ref` always overwrites (send an empty string to clear it).
Updating a peer automatically invalidates the southbound client pool entry so the next dispatch
rebuilds with the new endpoint and credentials.

```http
PUT /api/a2a/peers/a2a_3f8c9d1a2b4e5f60
Authorization: Bearer <admin-token>
Content-Type: application/json

{
  "endpoint": "https://research-v2.example.internal/a2a",
  "egress_auth_ref": "research-agent-bearer-v2"
}
```

### Disabling without removing

Set `"enabled": false` to stop dispatches to the peer without deleting its record or its card cache.
An Acquire call against a disabled peer returns `ErrPeerDisabled`, which the northbound transport
maps to JSON-RPC code `-32004` (`ErrUnsupportedOperation`).

### Deleting a peer

`DELETE /api/a2a/peers/{id}` returns `204` on success and also invalidates the pool entry. Remove
the peer name from any Agent Profile allowlists and bridge tables before or immediately after
deletion to avoid stale routing configuration.

---

## Console

All of the above is available through the **A2A → Peers** section of the Portico Console. The Peers
list shows each registered peer's name, endpoint, enabled status, and the time the agent card was
last refreshed. The "Refresh card" button triggers `POST /api/a2a/peers/{id}/refresh-card`
directly. Bridge configuration lives on the Agent Profile editor under the **Bridges** tab.

---

## Quick-reference: REST endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/a2a/peers` | List all peers (tenant-scoped) |
| `POST` | `/api/a2a/peers` | Register a new peer |
| `GET` | `/api/a2a/peers/{id}` | Get peer + cached agent card |
| `PUT` | `/api/a2a/peers/{id}` | Update peer fields |
| `DELETE` | `/api/a2a/peers/{id}` | Remove a peer |
| `POST` | `/api/a2a/peers/{id}/refresh-card` | Fetch and cache the peer's agent card |
| `GET` | `/a2a/.well-known/agent.json` | Portico's aggregated agent card |
| `POST` | `/a2a` | A2A JSON-RPC 2.0 endpoint (governed proxy) |

---

## Related

- [A2A overview](/concepts/a2a) — protocol concepts, the governed envelope, and the architecture of `internal/a2a/`
- [A2A bridges](/concepts/a2a-bridges) — MCP↔A2A cross-protocol routing in depth
- [Agent Profiles](/concepts/agent-profiles) — the full profile schema and entitlement model
- [Create an Agent Profile](/guides/create-agent-profile) — step-by-step profile creation
- [Credentials vault](/concepts/credentials-vault) — vault operations, key management, and the injector seam
- [Audit](/concepts/audit) — event types, query API, and retention
