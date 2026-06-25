# MCP & A2A methods

Portico implements two JSON-RPC 2.0 protocols on a single listener:

- **MCP (Model Context Protocol)** — the northbound surface that AI clients connect to. Backed by `internal/mcp/protocol/`.
- **A2A (Agent-to-Agent)** — the peer-routing surface that lets agents dispatch tasks to other agents. Backed by `internal/a2a/protocol/`.

Every method name and error code on this page is a constant in the respective protocol package; raw string literals for method names are forbidden in production code per the project's contributor rules. If a constant does not appear in this page it does not exist in Portico's implementation.

## Protocol versions

| Protocol | Constant | Value | Source |
|---|---|---|---|
| MCP | `ProtocolVersion` | `2025-11-25` | `internal/mcp/protocol/types.go` |
| A2A | `SpecVersion` | `0.2.5` | `internal/a2a/protocol/version.go` |
| JSON-RPC (both) | `JSONRPCVersion` | `2.0` | both protocol packages |

The `2025-11-25` MCP revision adds icon metadata on tools, resources, and prompts; an `Implementation.description` field; and the OIDC discovery hooks. Portico advertises this revision string in every `initialize` response. Bumping either version constant is an RFC-level change.

---

## MCP methods

### `initialize`

Establishes the session. The client sends its `protocolVersion`, `clientInfo`, and `capabilities`; Portico responds with the matching server capabilities, its own `serverInfo`, and an optional `instructions` string.

```json
// Client → server
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "elicitation": {}
    },
    "clientInfo": { "name": "my-agent", "version": "1.0.0" }
  }
}

// Server → client
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "tools": { "listChanged": true },
      "resources": { "subscribe": true, "listChanged": true },
      "prompts": { "listChanged": true }
    },
    "serverInfo": { "name": "portico", "version": "0.1.0" }
  }
}
```

Code Mode is opted into during `initialize` via the `experimental.portico.code_mode` capability. See [Code Mode](/concepts/code-mode) for the full opt-in shape.

After a successful `initialize` the client must send a `notifications/initialized` notification before issuing any other requests. Portico enforces this ordering.

### `ping`

A no-op round-trip used by clients to verify liveness. Portico responds with an empty result object.

### `tools/list`

Returns the paginated list of tools visible to the session. When Code Mode is active the response contains only the four `mcp.*` meta-tools rather than the namespaced catalog. Supports cursor-based pagination via `cursor` / `nextCursor`.

```jsonc
// Request
{ "method": "tools/list", "params": { "cursor": "<opaque>" } }

// Response (fragment)
{
  "result": {
    "tools": [
      {
        "name": "github.list_issues",
        "description": "...",
        "inputSchema": { "type": "object", "properties": { ... } },
        "annotations": { "readOnlyHint": true }
      }
    ],
    "nextCursor": "<opaque>"
  }
}
```

Each `Tool` object may carry an `icons` array (added in `2025-11-25`) and an `annotations` object with `readOnlyHint`, `destructiveHint`, `idempotentHint`, and `openWorldHint` booleans.

### `tools/call`

Invokes a single tool by namespaced name. The `arguments` field is passed verbatim to the downstream server after policy evaluation, credential injection, and (if required) approval gating.

```jsonc
// Request
{
  "method": "tools/call",
  "params": {
    "name": "github.list_issues",
    "arguments": { "repo": "acme/core", "state": "open" },
    "_meta": { "progressToken": 42 }
  }
}

// Response
{
  "result": {
    "content": [{ "type": "text", "text": "..." }],
    "isError": false
  }
}
```

`CallToolResult.content` is an array of `ContentBlock` objects. Each block carries a `type` (`"text"`, `"image"`, `"resource"`), and the optional `structuredContent` field carries a JSON-typed payload when the downstream server produces one.

If a tool is flagged `requires_approval` and the session's policy demands gating, Portico returns error code `-32001` (`ErrApprovalRequired`) instead of a result. See [Approvals](/concepts/approvals).

### `resources/list`

Returns the paginated list of resources the aggregated session can read. Supports cursor pagination.

### `resources/read`

Reads a single resource by URI. Returns one or more `ContentBlock` items.

### `resources/templates/list`

Returns URI templates that clients can expand to form valid resource URIs.

### `resources/subscribe` / `resources/unsubscribe`

Registers or cancels interest in update notifications for a specific resource URI. When subscribed, Portico forwards `notifications/resources/updated` events from the downstream server.

### `prompts/list`

Returns the paginated list of prompt templates visible to the session.

### `prompts/get`

Fetches a single prompt template by name, returning the rendered `messages` array.

---

## MCP notifications

Notifications are fire-and-forget JSON-RPC messages with no `id` field. Portico emits them server-to-client over the SSE channel.

| Notification method | Direction | Meaning |
|---|---|---|
| `notifications/initialized` | client → server | Session handshake complete; client is ready. |
| `notifications/cancelled` | client → server | Cancel a pending request by `requestId`. |
| `notifications/progress` | server → client | Progress update for a long-running `tools/call`. |
| `notifications/tools/list_changed` | server → client | The visible tool set changed; client should re-issue `tools/list`. |
| `notifications/resources/list_changed` | server → client | The resource list changed. |
| `notifications/resources/updated` | server → client | A subscribed resource has new content. |
| `notifications/prompts/list_changed` | server → client | The prompt list changed. |

`notifications/cancelled` carries `CancelledParams` with `requestId` (the ID of the in-flight request) and an optional `reason` string. Portico propagates the cancellation downstream where the transport supports it.

`notifications/progress` carries `ProgressParams` with `progressToken` (echoed from the originating `tools/call._meta`), a `progress` float, an optional `total` float, and an optional `message` string.

---

## Server-initiated requests: `elicitation/create`

Elicitation is the mechanism by which Portico (acting as server) asks the connected AI client to collect structured input from the end-user mid-call — for example to confirm a destructive action or supply a missing credential.

```jsonc
// Portico → client (over the SSE channel, with a request ID for the reply)
{
  "jsonrpc": "2.0",
  "id": "elicit-7a3f",
  "method": "elicitation/create",
  "params": {
    "message": "The deployment will overwrite production. Confirm?",
    "requestedSchema": {
      "type": "object",
      "properties": {
        "confirmed": { "type": "boolean" }
      },
      "required": ["confirmed"]
    }
  }
}

// Client → Portico (reply)
{
  "jsonrpc": "2.0",
  "id": "elicit-7a3f",
  "result": {
    "action": "accept",
    "content": { "confirmed": true }
  }
}
```

The `action` field is one of `"accept"`, `"reject"`, or `"cancel"`. Portico only advertises `elicitation/create` when the client's `initialize` capabilities included `elicitation: {}`. See [Approvals](/concepts/approvals) and [MCP northbound](/concepts/mcp-northbound) for the full flow.

---

## Code Mode meta-tools (`mcp.*` namespace)

When a session opts into Code Mode, `tools/list` returns four reserved meta-tools instead of the namespaced catalog. The `mcp.` namespace is exclusively for these tools; no other tool may be registered under it.

| Tool name | Purpose |
|---|---|
| `mcp.listToolFiles` | Enumerate the virtual `.pyi` stub files representing the session's tool catalog. |
| `mcp.readToolFile` | Read one virtual stub file for a server or tool (compact function signatures). |
| `mcp.getToolDocs` | Fetch full docs — description, JSON Schema, risk class, approval policy — for one or more named tools. |
| `mcp.executeToolCode` | Run a Starlark snippet that calls tools through their server modules inside a sandboxed interpreter. |

`mcp.executeToolCode` takes two mutually exclusive arguments:

- `code` — a Starlark source string. The snippet calls tools via server-module functions; the final value must be assigned to `result`, which is the only value returned to the caller.
- `continuation_token` — resumes a previously suspended execution after an approval gate was cleared.

If a tool called from inside the sandbox requires operator approval, `mcp.executeToolCode` returns error code `-32001` (`ErrApprovalRequired`) with a `continuation_token` in the error data. Once the approval is granted, the client resumes by calling `mcp.executeToolCode` again with that token instead of `code`.

See [Code Mode](/concepts/code-mode) for the full sandbox design and savings model.

---

## MCP error codes

All error codes are defined in `internal/mcp/protocol/errors.go`.

### JSON-RPC standard codes

| Code | Constant | Meaning |
|---|---|---|
| `-32700` | `ErrParseError` | Malformed JSON in the request body. |
| `-32600` | `ErrInvalidRequest` | Valid JSON but not a valid JSON-RPC object. |
| `-32601` | `ErrMethodNotFound` | No handler for the requested method. |
| `-32602` | `ErrInvalidParams` | Parameters failed schema validation. |
| `-32603` | `ErrInternalError` | Unhandled internal error. |
| `-32800` | `ErrCancelled` | The request was cancelled by the client. |

### Portico-defined codes

| Code | Constant | Meaning |
|---|---|---|
| `-32001` | `ErrApprovalRequired` | The tool call requires explicit operator approval before it can proceed. |
| `-32002` | `ErrUpstreamUnavailable` | The downstream MCP server transport failed or timed out. |
| `-32003` | `ErrPolicyDenied` | A policy rule denied the call outright (no approval path available). |
| `-32004` | `ErrToolNotEnabled` | The requested tool is not visible to this session (wrong namespace, not registered, or outside the Agent Profile's allowed surface). |
| `-32005` | `ErrTenantInactive` | The tenant associated with this session is inactive. |
| `-32006` | `ErrAgentProfileViolation` | The tool, alias, or skill is outside the session's Agent Profile surface. See [Agent Profiles](/concepts/agent-profiles). |
| `-32007` | `ErrVKScopeViolation` | The tool or server is outside the Virtual Key's configured allowlist. See [Virtual Keys](/concepts/virtual-keys). |

### Code Mode error codes

| Code | Constant | Meaning |
|---|---|---|
| `-32010` | `ErrCodeModeUnsafe` | The static safety gate rejected the Starlark snippet before execution began. |
| `-32011` | `ErrCodeModeBudget` | An execution budget (time, tool-call count) was exceeded inside the sandbox. |
| `-32012` | `ErrCodeModeExecution` | A compile-time or runtime error occurred inside the sandbox, or an in-sandbox tool call failed. |

Code Mode errors carry structured `data` with a `code` field (the specific `code_mode.*` reason string) and an optional `detail` string. The top-level JSON-RPC code groups errors by class; the `data.code` field gives the precise cause.

---

## A2A methods

A2A uses the same JSON-RPC 2.0 envelope as MCP. All method constants are in `internal/a2a/protocol/methods.go`. A2A traffic reaches Portico at `/a2a/` — a single listener shared across all tenants, with tenant identity resolved from the bearer token.

See [A2A overview](/concepts/a2a) for the full architecture, and [A2A bridges](/concepts/a2a-bridges) for the MCP-to-A2A forwarding path.

### Message methods

| Method | Direction | Purpose |
|---|---|---|
| `message/send` | client → agent | Create a task (or add a turn to an existing one) and return a synchronous result. |
| `message/stream` | client → agent | Same as `message/send` but returns a streaming SSE channel for long-running tasks. Requires the agent to advertise `capabilities.streaming: true` in its AgentCard. |

`message/send` accepts `MessageSendParams`:

```jsonc
{
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "parts": [{ "kind": "text", "text": "Summarise the Q3 report" }],
      "messageId": "msg-001",
      "contextId": "ctx-abc"
    },
    "configuration": {
      "acceptedOutputModes": ["text/plain"],
      "blocking": false
    }
  }
}
```

The `Message.parts` array carries `Part` objects discriminated by `kind`: `"text"`, `"file"`, or `"data"`. A `"file"` part carries a `FileContent` with either inline base-64 `bytes` or a `uri` to fetch from. A `"data"` part carries an arbitrary JSON-typed `data` map.

The response is a `Task` object. Its `status.state` is one of: `submitted`, `working`, `input-required`, `completed`, `canceled`, `failed`, `rejected`, `auth-required`, or `unknown`.

### Task methods

| Method | Purpose |
|---|---|
| `tasks/get` | Retrieve a task by `id`, optionally capping history with `historyLength`. |
| `tasks/cancel` | Request cancellation of a task by `id`. Returns `ErrTaskNotCancelable` if the task is already in a terminal state. |
| `tasks/resubscribe` | Re-attach a streaming SSE subscription to an existing task (e.g. after a dropped connection). |

### Push notification config methods

These four methods manage webhooks for asynchronous task state updates on agents that advertise `capabilities.pushNotifications: true`.

| Method | Purpose |
|---|---|
| `tasks/pushNotificationConfig/set` | Register a webhook target for a task. |
| `tasks/pushNotificationConfig/get` | Retrieve the registered webhook config for a task. |
| `tasks/pushNotificationConfig/list` | List all push notification configs for the calling tenant. |
| `tasks/pushNotificationConfig/delete` | Remove a webhook config. |

### Agent card method

| Method | Purpose |
|---|---|
| `agent/getAuthenticatedExtendedCard` | Return the extended variant of the AgentCard populated with additional fields visible only to authenticated callers. Requires the AgentCard to declare `supportsAuthenticatedExtendedCard: true`. |

---

## A2A error codes

All codes are defined in `internal/a2a/protocol/errors.go`.

### JSON-RPC standard codes

| Code | Constant | Meaning |
|---|---|---|
| `-32700` | `ErrParseError` | Malformed JSON. |
| `-32600` | `ErrInvalidRequest` | Invalid JSON-RPC structure. |
| `-32601` | `ErrMethodNotFound` | Unknown method. |
| `-32602` | `ErrInvalidParams` | Parameter validation failed. |
| `-32603` | `ErrInternalError` | Unhandled internal error. |

### A2A-specific codes

| Code | Constant | Meaning |
|---|---|---|
| `-32001` | `ErrTaskNotFound` | The referenced `task_id` does not exist. |
| `-32002` | `ErrTaskNotCancelable` | The task is already in a terminal state and cannot be cancelled. |
| `-32003` | `ErrPushNotificationNotSupported` | The peer does not implement push notification config. |
| `-32004` | `ErrUnsupportedOperation` | The requested operation is not supported by this peer. |
| `-32005` | `ErrContentTypeNotSupported` | The `Part.contentType` is not acceptable to this peer. |
| `-32006` | `ErrInvalidAgentResponse` | The agent returned a response that could not be parsed or validated. |

### Portico-defined A2A code

| Code | Constant | Meaning |
|---|---|---|
| `-32010` | `ErrProfileViolation` | The caller's Agent Profile does not permit interaction with this peer or task type. See [Agent Profiles](/concepts/agent-profiles). |

---

## A2A AgentCard

The AgentCard is A2A's discovery document, served at `/.well-known/agent.json` (per-agent). It is the A2A analog of the MCP `initialize` response: enough for a client to decide whether and how to connect.

Key fields (from `internal/a2a/protocol/agent_cards.go`):

| Field | Type | Purpose |
|---|---|---|
| `name` | string | Human-readable agent name. |
| `url` | string | The A2A endpoint clients POST JSON-RPC to. |
| `version` | string | Agent's own version string (independent of `protocolVersion`). |
| `protocolVersion` | string | Should match `SpecVersion` (`0.2.5`). |
| `capabilities` | `AgentCapabilities` | Flags for `streaming`, `pushNotifications`, and `stateTransitionHistory`. |
| `skills` | `[]AgentSkill` | Discoverable skill surfaces with `inputModes`/`outputModes`. |
| `supportsAuthenticatedExtendedCard` | bool | Whether `agent/getAuthenticatedExtendedCard` is available. |

---

## Wire envelope

Both protocols share the same JSON-RPC 2.0 envelope shapes. Requests carry `jsonrpc`, `id`, `method`, and `params`. Notifications omit `id`. Responses carry either `result` or `error` (never both). The `id` field is a `json.RawMessage` to preserve the wire form (string, number, or null) without coercion.

```go
// Both protocols share this shape (from their respective types.go)
type Request struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}
```

An absent or `null` `id` marks a notification; Portico's `IsNotification()` helper encodes this check for handlers.

---

## Related

- [MCP northbound transport](/concepts/mcp-northbound) — HTTP+SSE transport, session lifecycle, authentication, SSE resumption.
- [MCP southbound clients](/concepts/mcp-southbound) — how Portico connects to downstream servers as a client.
- [A2A overview](/concepts/a2a) — task model, AgentCard ingestion, peer routing, and multi-tenant isolation.
- [A2A bridges](/concepts/a2a-bridges) — how `tools/call` requests are forwarded to A2A peers and how responses map back to MCP content.
- [Code Mode](/concepts/code-mode) — Starlark sandbox, stub projection, approval suspension and resumption.
- [Approvals](/concepts/approvals) — policy-gated tool calls, elicitation flow, continuation model.
- [Agent Profiles](/concepts/agent-profiles) — per-consumer allowed server/tool/skill/model surfaces enforced at the dispatch layer.
- [Virtual Keys](/concepts/virtual-keys) — scoped API keys with server and tool allowlists (`ErrVKScopeViolation`).
- [REST API reference](/reference/rest-api) — the HTTP management and Code Mode Console endpoints.
