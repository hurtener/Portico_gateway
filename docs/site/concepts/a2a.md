# A2A overview

Agent-to-Agent (A2A) is the second agentic wire protocol Portico speaks, alongside MCP. Both run on the **same listener**, behind the **same governance envelope**: tenant identity, Virtual Key or JWT authentication, Agent Profile entitlement, policy, vault-managed egress credentials, and structured audit. Adding A2A to an existing Portico deployment is strictly additive — a tenant that registers no peers and declares no bridges behaves identically to before.

## How A2A differs from MCP

MCP is a tool-and-resource protocol. An MCP server exposes a catalogue of discrete **tools**, **resources**, and **prompts** that an AI client calls by name. Portico's MCP gateway aggregates those catalogues and applies governance to every call.

A2A is a task-and-message protocol. An A2A agent publishes an **agent card** describing the skills it offers, and clients send it **messages** that the agent turns into **tasks**. Tasks have a lifecycle (`submitted` → `working` → `completed` or a terminal error state). Results come back as **artifacts** — named bundles of typed parts (text, file, or structured data).

The conceptual mapping:

| MCP | A2A |
|-----|-----|
| `tools/list` response | Agent card (`GET /.well-known/agent.json`) |
| Tool | Skill (groups related tasks) |
| `tools/call` | `message/send` (creates or continues a task) |
| Tool result | Artifact (one or more typed parts) |
| `resources/read` | `tasks/get` (retrieve a task and its history) |

Portico holds both protocol surfaces in one process. An Agent Profile can configure **MCP→A2A bridges** so a calling agent that knows only MCP tools can transparently dispatch work to a remote A2A peer. See [A2A bridges](/concepts/a2a-bridges) for that routing model.

## Protocol version

Portico pins a specific A2A spec revision. The constant `SpecVersion` in `internal/a2a/protocol/version.go` is currently `"0.2.5"`. Bumping the version is an RFC change, not a routine code edit. All A2A wire types — the JSON-RPC 2.0 envelope, agent card, task, message, part, artifact — are defined **only** in `internal/a2a/protocol`. No other package in the repository defines A2A message structs.

## The two A2A endpoints

### Discovery: agent card

```http
GET /a2a/.well-known/agent.json
Authorization: Bearer <token>
```

Returns Portico's `AgentCard` for the authenticated tenant. The card aggregates the skills discovered from the tenant's registered peers (populated after a card refresh) together with Portico's own identity and capabilities. An external A2A client fetches this document to understand what Portico can do on behalf of the tenant.

Example response (abbreviated):

```json
{
  "name": "Portico gateway",
  "url": "https://gateway.example.com/a2a",
  "version": "1.0.0",
  "protocolVersion": "0.2.5",
  "capabilities": {
    "streaming": false,
    "pushNotifications": false,
    "stateTransitionHistory": false
  },
  "skills": [
    {
      "id": "code-review",
      "name": "Code review",
      "description": "Review a pull request and return structured findings.",
      "inputModes": ["text/plain"],
      "outputModes": ["application/json"]
    }
  ]
}
```

The `AgentCard` type (defined in `internal/a2a/protocol/agent_cards.go`) carries: `name`, `description`, `url`, `version`, `protocolVersion`, `provider`, `capabilities`, `defaultInputModes`, `defaultOutputModes`, `skills`, `documentationUrl`, and `supportsAuthenticatedExtendedCard`.

### Dispatch: JSON-RPC endpoint

```http
POST /a2a
Authorization: Bearer <token>
Content-Type: application/json
```

All A2A JSON-RPC calls arrive here. The body is a JSON-RPC 2.0 request:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "messageId": "msg-01",
      "parts": [{ "kind": "text", "text": "Review PR #42 against the security checklist." }]
    },
    "metadata": {
      "portico_peer": "research-agent"
    }
  }
}
```

The `params.metadata.portico_peer` key names the registered peer to dispatch to. It is required for every call; the transport returns `ErrInvalidParams` (-32602) if it is absent.

::: info Same JSON-RPC 2.0 framing for every method
A2A is a JSON-RPC 2.0 protocol end to end. Errors are carried in the response body with a 200 HTTP status — the transport layer succeeded even when the application layer did not. Portico echoes the request `id` on every response.
:::

## Supported methods

| Method | Description |
|--------|-------------|
| `message/send` | Send a message; creates or continues a task on the peer. |
| `tasks/get` | Retrieve a task by ID, including history and artifacts. |
| `tasks/cancel` | Request cancellation of a running task. |
| `message/stream` | Send a message and receive a server-sent event stream of incremental updates (peer capability). |
| `tasks/resubscribe` | Re-attach to an existing task's event stream after a disconnect. |
| `tasks/pushNotificationConfig/set` | Register a webhook URL for task state updates. |
| `tasks/pushNotificationConfig/get` | Retrieve the current push-notification configuration. |
| `tasks/pushNotificationConfig/list` | List all registered push-notification configurations. |
| `tasks/pushNotificationConfig/delete` | Remove a push-notification configuration. |
| `agent/getAuthenticatedExtendedCard` | Fetch the peer's extended agent card (visible only to authenticated callers). |

Method name constants live in `internal/a2a/protocol/methods.go`. Raw string literals for method names are forbidden throughout the codebase — only those constants may be used.

## Wire types

### Task lifecycle

A task moves through the following states (exact wire strings per the A2A spec):

| State | Meaning |
|-------|---------|
| `submitted` | Task received but not yet picked up. |
| `working` | Agent is actively processing. |
| `input-required` | Agent is waiting for additional input from the caller. |
| `completed` | Task finished successfully; artifacts are available. |
| `canceled` | Caller or agent canceled the task. |
| `failed` | Agent encountered an unrecoverable error. |
| `rejected` | Agent refused to execute the task. |
| `auth-required` | Agent requires authentication that was not provided. |
| `unknown` | State cannot be determined. |

### Message parts

A `Message` carries one or more `Part` values. The `kind` field discriminates the payload:

| `kind` | Payload field | Description |
|--------|---------------|-------------|
| `text` | `text` | Plain or structured text. |
| `file` | `file` | Inline base64 bytes (`bytes`) or a URI (`uri`), plus `mimeType` and `name`. |
| `data` | `data` | Arbitrary JSON object (structured data, tool output, etc.). |

Every part may also carry a free-form `metadata` map for application-level annotations.

### Artifacts

When a task completes, the agent returns zero or more `Artifact` values — each with an `artifactId`, an optional `name` and `description`, and a list of `Part` values. Long-running tasks may stream artifact increments before completion.

## The governance envelope

Every A2A call — whether it arrives directly at `POST /a2a` or enters through an MCP→A2A bridge — traverses the same sequence:

```
Inbound request
    │
    ▼
Tenant + identity
  JWT or Virtual Key validation (same auth middleware as /mcp and /v1)
    │
    ▼
Agent Profile entitlement  (internal/a2a/dispatch)
  Profile.AllowsA2APeer(peerName)
  → deny: ErrProfileViolation (-32010) + agent_profile.violation audit event
    │
    ▼
Vault egress credentials
  peer.egress_auth_ref resolved → Bearer header on outbound request
  Inbound Authorization is never forwarded
    │
    ▼
Southbound dispatch  (internal/a2a/southbound/http client)
  pooled, per-(tenant, peer) HTTP client
    │
    ▼
Audit  (a2a.dispatch event)
```

The northbound transport (`internal/a2a/northbound/http`) is mounted inside the auth middleware group, so by the time a request reaches the JSON-RPC handler, tenant identity and the resolved Agent Profile are already in the request context.

### Agent Profile entitlement

The [Agent Profile](/concepts/agent-profiles) is the single source of truth for what a consumer is permitted to do. For A2A:

- `AllowsA2APeer(name)` — gates which registered peers the consumer may reach. Checked in `internal/a2a/dispatch` for every call.
- `AllowsA2ATask("peer.task")` — gates specific task names. Checked in the bridge layer, where the target task is explicit.

An empty (nil or default) profile permits all peers — preserving backward compatibility for deployments that predate Agent Profiles.

Profile violations produce error code **-32010** (`ErrProfileViolation`) on the wire and emit an `agent_profile.violation` audit event. The error's `data` field carries `profile_id`, `peer`, and `reason`.

### Egress credentials and the vault

A registered peer may carry an `egress_auth_ref` — a reference to a secret stored in Portico's credentials vault. When the dispatcher acquires a southbound client for the peer, the pool resolves that reference to a `Bearer` token and attaches it to every outbound request. Credentials never leave the gateway to the caller; they are injected only on the outbound hop. See [Credentials vault](/concepts/credentials-vault) for how secrets are stored and rotated.

## Peer management

Peers are tenant-scoped resources stored in Portico's database. The management REST surface is:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/a2a/peers` | List all registered peers for the tenant. |
| `POST` | `/api/a2a/peers` | Register a new peer. |
| `GET` | `/api/a2a/peers/{id}` | Get a peer, including its cached agent card JSON. |
| `PUT` | `/api/a2a/peers/{id}` | Update peer configuration. |
| `DELETE` | `/api/a2a/peers/{id}` | Remove a peer. |
| `POST` | `/api/a2a/peers/{id}/refresh-card` | Fetch the peer's card and persist it. |

`POST /api/a2a/peers/{id}/refresh-card` triggers the ingest layer (`internal/a2a/ingest`). The refresher derives the card URL from the peer's endpoint by replacing the path with `/.well-known/agent.json`, GETs it over the governed southbound client, and stores the raw JSON on the peer row. Once cached, the card's skills are reflected in both Portico's own agent card and the tenant's catalog.

Peer management is available in the Console under **A2A → Peers**.

## Error codes

Error codes are defined in `internal/a2a/protocol/errors.go`. They group into three ranges:

### Standard JSON-RPC 2.0

| Code | Constant | Meaning |
|------|----------|---------|
| -32700 | `ErrParseError` | Request body could not be parsed as JSON. |
| -32600 | `ErrInvalidRequest` | The JSON-RPC request object is invalid. |
| -32601 | `ErrMethodNotFound` | The requested method is not implemented. |
| -32602 | `ErrInvalidParams` | Parameters are structurally invalid or missing. |
| -32603 | `ErrInternalError` | Unspecified server-side error. |

### A2A spec codes (-32001 to -32006)

| Code | Constant | Meaning |
|------|----------|---------|
| -32001 | `ErrTaskNotFound` | Referenced `task_id` does not exist. |
| -32002 | `ErrTaskNotCancelable` | Task is already in a terminal state. |
| -32003 | `ErrPushNotificationNotSupported` | Peer does not implement push notifications. |
| -32004 | `ErrUnsupportedOperation` | Requested operation is not supported by this peer. |
| -32005 | `ErrContentTypeNotSupported` | A `Part` content type is not acceptable to the peer. |
| -32006 | `ErrInvalidAgentResponse` | Peer returned a response that could not be parsed or validated. |

### Portico-defined codes

| Code | Constant | Meaning |
|------|----------|---------|
| -32010 | `ErrProfileViolation` | Caller's Agent Profile does not permit access to this peer or task. |

## Back-compatibility guarantee

A2A is entirely additive. No existing MCP or LLM gateway behavior changes. The `/a2a` routes are mounted alongside — not instead of — `/mcp` and `/v1`. A tenant with no registered peers and no A2A bridges on any Agent Profile is unaffected. This means A2A can be enabled at any time without a coordinated rollout or a configuration migration.

## Architecture reference

```
internal/a2a/
├── protocol/          # wire types (single source of truth)
│   ├── version.go     #   SpecVersion constant
│   ├── types.go       #   JSON-RPC 2.0 envelope
│   ├── agent_cards.go #   AgentCard, AgentSkill, AgentProvider
│   ├── capabilities.go#   AgentCapabilities
│   ├── tasks.go       #   Task, Message, Part, Artifact, TaskStatus
│   ├── methods.go     #   method-name constants
│   └── errors.go      #   error codes + NewError helper
├── northbound/
│   └── http/          # inbound transport (POST /a2a, GET agent card)
├── southbound/
│   ├── types.go       #   southbound.Client interface
│   ├── http/          #   concrete HTTP client
│   └── manager/       #   per-(tenant,peer) connection pool
├── dispatch/          # governed dispatch: profile + pool + audit
└── ingest/            # agent card refresh and persistence
```

::: warning Wire types belong in one place
`internal/a2a/protocol` is the single source of truth for A2A message types. No other package may define A2A structs. This mirrors the same rule for MCP types in `internal/mcp/protocol`.
:::

## Related

- [A2A bridges](/concepts/a2a-bridges) — how MCP `tools/call` dispatches transparently to an A2A peer, and the reverse direction.
- [Agent Profiles](/concepts/agent-profiles) — consumer entitlement for peers, tasks, and bridges.
- [Credentials vault](/concepts/credentials-vault) — how egress authentication secrets are stored and injected.
- [Authentication](/concepts/authentication) — JWT and Virtual Key validation that gates every `/a2a` route.
- [Audit](/concepts/audit) — `a2a.dispatch` and `agent_profile.violation` event shapes.
- [MCP gateway](/concepts/mcp-gateway) — the sibling protocol surface on the same listener.
- [Setup an A2A peer](/guides/setup-a2a-peer) — step-by-step guide to registering a peer and issuing a governed call.
- [REST API reference](/reference/rest-api) — full peer management endpoint specifications.
