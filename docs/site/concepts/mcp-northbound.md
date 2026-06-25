# MCP northbound (clients)

The northbound surface is the side of the MCP gateway that clients talk to. It is the transport layer between an AI client — an IDE plugin, an agentic framework, a desktop host — and the rest of the gateway. Everything inward from the client (credential injection, policy evaluation, southbound fan-out) is invisible to the client; northbound is the single interface clients interact with.

This page covers the wire protocol, session lifecycle, capability negotiation, notifications, cancellation, progress, the `_meta` extension field, Origin enforcement, and the server-initiated request (elicitation) mechanism.

For a broader view of where northbound fits in the full request path, see [MCP Gateway](/concepts/mcp-gateway) and [Architecture](/concepts/architecture). For the outbound half of the gateway, see [MCP southbound](/concepts/mcp-southbound).

---

## Protocol version

Portico targets MCP protocol revision **`2025-11-25`**, pinned as `ProtocolVersion` in `internal/mcp/protocol/types.go`. Every `initialize` response carries this version string. Bumping the version requires an RFC change; it is not a code-level decision.

The `2025-11-25` revision introduced:
- Icon metadata on tools, resources, and prompts.
- An `Implementation.description` field in server info.
- Mandatory Origin 403 enforcement for browser clients.
- Clarifications to Streamable HTTP SSE resumption semantics.
- The `elicitation` client capability.

---

## HTTP surface

The northbound transport is Streamable HTTP. Three verbs are mounted at `/mcp`:

| Verb | Purpose | Success status |
|---|---|---|
| `POST /mcp` | Send a JSON-RPC request or notification | 200 (request), 202 (notification or elicitation reply) |
| `GET /mcp` | Open a persistent SSE channel for server-to-client notifications | 200 (streams until session ends or client disconnects) |
| `DELETE /mcp` | Terminate the session explicitly | 204 |

All three verbs require a `Mcp-Session-Id` header except the first `POST /mcp` that carries an `initialize` request — that request creates the session and the response carries the session ID back.

Request bodies are limited to **8 MiB**. Larger payloads are silently truncated before JSON parsing.

---

## Session lifecycle

### 1. Initialize handshake

A fresh session always begins with an `initialize` request. No other method is accepted without a valid session ID.

```http
POST /mcp HTTP/1.1
Content-Type: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "elicitation": {}
    },
    "clientInfo": {
      "name": "my-agent",
      "version": "1.0.0"
    }
  }
}
```

The gateway responds with `200 OK`, sets a `Mcp-Session-Id` header, and returns the aggregated server capabilities:

```http
HTTP/1.1 200 OK
Content-Type: application/json
Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE

{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "tools": { "listChanged": true },
      "resources": { "listChanged": true, "subscribe": false },
      "prompts": { "listChanged": true }
    },
    "serverInfo": {
      "name": "portico-gateway",
      "version": "phase-3.5",
      "description": "Portico — multi-tenant MCP gateway and Skill runtime"
    }
  }
}
```

### 2. Subsequent requests

Every subsequent request includes the session ID:

```http
POST /mcp HTTP/1.1
Content-Type: application/json
Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE

{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/list",
  "params": {}
}
```

A request without `Mcp-Session-Id` (other than `initialize`) returns a `404` JSON-RPC error with the message `"session not found"` and a hint to include the header.

### 3. The initialized notification

After receiving the `initialize` result, the client must send a `notifications/initialized` notification. This is a fire-and-forget; the gateway acknowledges it with `202 Accepted`.

```http
POST /mcp HTTP/1.1
Content-Type: application/json
Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE

{
  "jsonrpc": "2.0",
  "method": "notifications/initialized"
}
```

### 4. Session termination

An explicit `DELETE /mcp` terminates the session:

```http
DELETE /mcp HTTP/1.1
Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE
```

The gateway responds with `204 No Content`, cancels all in-flight requests for that session, closes the SSE channel if one is open, and releases the session from the registry.

Session IDs are random, prefixed `s_`, and base64url-encoded (e.g. `s_Ak3xVwLm9pQr7TcE`).

---

## Capability negotiation

Portico advertises the union of capabilities across all registered downstream MCP servers visible to the session's tenant. The aggregation runs at `initialize` time: each downstream's `ServerCapabilities` is fetched and merged via `AggregateServerCaps` in `internal/mcp/protocol/capabilities.go`.

The rules:
- `tools` is always advertised (even with no downstream configured).
- `tools.listChanged`, `resources.listChanged`, `resources.subscribe`, and `prompts.listChanged` are `true` if at least one downstream advertises them.
- `logging` is advertised if at least one downstream advertises it.
- Experimental fields (`capabilities.experimental`) are preserved as-is and passed through to the client.

Client capabilities (`ClientCapabilities`) are recorded on the session at `initialize` time. The dispatcher reads `HasElicitation`, `HasSampling`, and `HasRoots` from the recorded caps to decide whether to attempt elicitation or fall back to a structured error.

### Code Mode opt-in

Clients that want Code Mode pass a Portico-specific experimental capability:

```json
{
  "capabilities": {
    "experimental": {
      "portico": {
        "code_mode": { "sandbox": "starlark" }
      }
    }
  }
}
```

A session without this experimental field sees the normal namespaced tool catalog; Code Mode tooling is never injected unless explicitly requested. See [Code Mode](/concepts/code-mode) for the full Code Mode surface.

### List-changed mode

By default, when a downstream emits a `notifications/tools/list_changed` (or resources/prompts equivalent), the gateway suppresses the notification and invalidates its internal cache — clients see the updated list on their next `tools/list` call without any extra fan-out. This is the **stable** mode.

Clients that prefer real-time push can opt into **live** mode:

```json
{
  "capabilities": {
    "experimental": {
      "portico": {
        "listChanged": "live"
      }
    }
  }
}
```

In live mode the notification is forwarded immediately to the client's SSE channel with the originating server ID annotated under `_meta.portico.serverID` in the params.

---

## The SSE notification channel

`GET /mcp` opens a long-lived Server-Sent Events stream for the session. The client must send `Accept: text/event-stream`; the gateway rejects the request with `406 Not Acceptable` otherwise.

```http
GET /mcp HTTP/1.1
Accept: text/event-stream
Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE
```

The response sets `Cache-Control: no-cache`, `Connection: keep-alive`, and `X-Accel-Buffering: no` (to disable nginx-style proxy buffering) before streaming events.

### Event IDs

Each SSE event carries an `id` field of the form `<sessionID>-<n>` where `n` is a per-stream monotonic counter:

```
id: s_Ak3xVwLm9pQr7TcE-1
data: {"jsonrpc":"2.0","method":"notifications/tools/list_changed","params":{}}

id: s_Ak3xVwLm9pQr7TcE-2
data: {"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"tok_1","progress":0.5,"message":"processing…"}}
```

::: info SSE resumption
Resumption (RFC 6202 `Last-Event-ID`) resets the counter — the gateway does not maintain a replay buffer. On reconnect the client starts a fresh sequence from 1. If notifications were emitted while the channel was disconnected, they were dropped; the client should issue fresh `tools/list` / `resources/list` calls after reconnecting to sync state.
:::

The notification channel is backed by a bounded buffer of 256 entries per session. When the buffer is full, the oldest notification is evicted to make room (drop-oldest policy). This condition is logged at `Warn` level; a well-behaved client keeps its SSE channel open and drains it promptly.

---

## Supported methods

All method name constants are defined in `internal/mcp/protocol/methods.go`. The table below covers every method the dispatcher handles in the current build:

| Method | Description |
|---|---|
| `initialize` | Open session, negotiate capabilities |
| `ping` | Liveness check; responds with `{}` |
| `tools/list` | List tools visible to the session (paginated via `cursor`) |
| `tools/call` | Invoke a tool |
| `resources/list` | List resources |
| `resources/read` | Read a resource by URI |
| `resources/templates/list` | List resource URI templates |
| `resources/subscribe` | Subscribe to resource update notifications |
| `resources/unsubscribe` | Unsubscribe |
| `prompts/list` | List prompt templates |
| `prompts/get` | Fetch a prompt by name |

Any method not in this list returns JSON-RPC error code `-32601` (`MethodNotFound`) with data `{"method": "<name>"}`.

---

## Cancellation

The client cancels an in-flight request by sending a `notifications/cancelled` notification:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/cancelled",
  "params": {
    "requestId": 42,
    "reason": "user interrupted"
  }
}
```

The gateway looks up the request ID in the session's cancel registry and invokes the associated `context.CancelFunc`. The running southbound call receives a cancelled context and terminates. The original request's response may still arrive (with an error) — cancellation is best-effort, not a transaction rollback.

---

## Progress notifications

`tools/call` supports streamed progress updates. The client signals interest by including a `progressToken` in the request's `_meta` field:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "acme__run-job",
    "arguments": { "jobId": "abc123" },
    "_meta": {
      "progressToken": "tok_7"
    }
  }
}
```

While the tool executes, the downstream server may emit progress updates. The gateway propagates them to the northbound SSE channel as `notifications/progress`:

```
id: s_Ak3xVwLm9pQr7TcE-3
data: {
  "jsonrpc":"2.0",
  "method":"notifications/progress",
  "params": {
    "progressToken": "tok_7",
    "progress": 0.33,
    "total": 1.0,
    "message": "step 1 of 3 complete"
  }
}
```

Progress is delivered only when the client has an open SSE channel. Requests without a `progressToken` never emit progress events.

---

## The `_meta` extension field

`_meta` is the MCP specification's general-purpose extension envelope. Portico uses it in two directions:

**Client → gateway** (`tools/call` params):

| Key | Type | Use |
|---|---|---|
| `_meta.progressToken` | string \| number | Identifies the progress stream for this invocation |
| `_meta.traceparent` | string | W3C Trace Context `traceparent` header; if provided, the gateway links its spans to the upstream trace |

**Gateway → client** (notification params, live list-changed mode):

| Key | Type | Use |
|---|---|---|
| `_meta.portico.serverID` | string | Identifies which downstream server emitted the list-changed event |

The HTTP-level `traceparent` header (standard W3C Trace Context) is also honored for trace propagation — the transport layer reads it before the dispatcher runs, so all spans for a request are automatically correlated with the caller's trace when that header is present.

---

## Origin 403 enforcement

Per the MCP `2025-11-25` spec, the gateway enforces an explicit allowlist for browser-originated requests. Any request that carries an `Origin` header must match the configured list; non-matching origins receive `403 Forbidden` with a JSON-RPC error body before any session state is touched.

Configure the allowlist in `portico.yaml`:

```yaml
server:
  bind: "0.0.0.0:8080"
  allowed_origins:
    - "https://console.example.com"
    - "https://app.example.com"
```

A wildcard `"*"` permits any origin. **Do not use `"*"` in production** — it removes the cross-origin protection the spec requires.

Requests that carry no `Origin` header (programmatic clients, CLI tools, server-to-server calls) always pass the origin check regardless of the allowlist. This means a well-configured production deployment can be lockdown-strict for browser origins while remaining fully open to non-browser agents.

In dev mode (`portico dev`), the gateway automatically permits all localhost origins (`localhost`, `127.0.0.1`, `[::1]` on any port) so the SvelteKit Console dev server can reach the gateway without configuration.

::: tip Testing the origin guard
```bash
# Should 403:
curl -X POST http://localhost:18080/mcp \
  -H "Origin: https://evil.example.com" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}'

# Should 200 (no Origin header — programmatic client):
curl -X POST http://localhost:18080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}'
```
:::

---

## Server-initiated requests (elicitation)

Elicitation is the MCP mechanism for the server to ask the client a question mid-flow — for example, to obtain approval for a tool call, to request a missing credential, or to gather additional parameters. The client must have declared `"elicitation": {}` in its `initialize` capabilities for elicitation to be available; otherwise the gateway falls back to a structured `ErrApprovalRequired` JSON-RPC error.

### How it works

Elicitation uses the request correlator in `internal/mcp/northbound/http/server_initiated.go`:

1. **Gateway calls `ServerInitiatedRequester.Send`** with a session ID, an MCP method (e.g. `elicitation/create`), and typed params.
2. **The correlator assigns a server-controlled ID** (prefixed `s_`, 12 random bytes) and registers a pending entry before writing to the session's notification channel.
3. **The SSE emitter detects the internal marker** (`_portico/server_request`) and emits the envelope as an `event: server_request` SSE event:

```
event: server_request
id: s_Ak3xVwLm9pQr7TcE-4
data: {
  "jsonrpc": "2.0",
  "id": "s_BxQ9vLmT2cZk",
  "method": "elicitation/create",
  "params": {
    "message": "The tool 'acme__delete-record' requires approval. Proceed?",
    "requestedSchema": {
      "type": "object",
      "properties": {
        "confirm": { "type": "boolean" }
      },
      "required": ["confirm"]
    }
  }
}
```

4. **The client POSTs a normal JSON-RPC response** back on `POST /mcp` with the matching `id`. The transport layer calls `TryDeliver` before dispatching to the request handler. When the ID matches a pending entry, `TryDeliver` consumes it and returns `202 Accepted`; the request never reaches the regular dispatcher.

```http
POST /mcp HTTP/1.1
Content-Type: application/json
Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE

{
  "jsonrpc": "2.0",
  "id": "s_BxQ9vLmT2cZk",
  "result": {
    "action": "accept",
    "content": { "confirm": true }
  }
}
```

5. **`Send` unblocks** and returns the response to the approval flow, which proceeds or aborts accordingly.

### Timeout and cleanup

A background sweeper runs every 30 seconds and rejects pending requests whose deadline has passed. `ServerInitiatedRequester.Stop()` (called on gateway shutdown) drains the pending map and delivers an error to every blocked `Send` call so goroutines are not leaked.

### Elicitation actions

The client may respond with one of three actions:

| Action | Meaning |
|---|---|
| `accept` | User provided input; `content` carries the field values |
| `reject` | User explicitly declined; the gateway aborts the tool call |
| `cancel` | User dismissed without deciding; the gateway may retry or surface the error |

When a client does not advertise the `elicitation` capability, the policy pipeline surfaces `ErrApprovalRequired` (code `-32001`) as a JSON-RPC error instead of attempting elicitation. See [Approvals](/concepts/approvals) for the full approval flow.

---

## JSON-RPC error codes

All error codes are defined in `internal/mcp/protocol/errors.go`. The table below covers the codes a northbound client may encounter:

| Code | Constant | Meaning |
|---|---|---|
| `-32700` | `ErrParseError` | Request body is not valid JSON |
| `-32600` | `ErrInvalidRequest` | Malformed JSON-RPC envelope (wrong verb, missing session, bad Origin) |
| `-32601` | `ErrMethodNotFound` | Requested method is not implemented in this build |
| `-32602` | `ErrInvalidParams` | Params failed schema validation |
| `-32603` | `ErrInternalError` | Unexpected server-side error |
| `-32800` | `ErrCancelled` | Request was cancelled by the client |
| `-32001` | `ErrApprovalRequired` | Tool requires human approval; elicitation is not available |
| `-32002` | `ErrUpstreamUnavailable` | Downstream server transport error |
| `-32003` | `ErrPolicyDenied` | Policy engine denied the request |
| `-32004` | `ErrToolNotEnabled` | Tool is not visible under this session's Agent Profile or namespace |
| `-32006` | `ErrAgentProfileViolation` | Tool, alias, or skill is outside the Agent Profile's allowed surface |
| `-32007` | `ErrVKScopeViolation` | Server or tool is outside the Virtual Key's allowlist |
| `-32010` | `ErrCodeModeUnsafe` | Code Mode: static safety gate rejected the snippet |
| `-32011` | `ErrCodeModeBudget` | Code Mode: execution budget limit reached |
| `-32012` | `ErrCodeModeExecution` | Code Mode: compile, runtime, or in-sandbox tool error |

All error responses follow the standard JSON-RPC shape:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "error": {
    "code": -32001,
    "message": "approval required",
    "data": { "tool": "acme__delete-record", "hint": "declare elicitation capability" }
  }
}
```

---

## Complete initialize exchange (annotated)

```jsonc
// 1. Client → gateway: initialize
POST /mcp
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",      // must match ProtocolVersion
    "capabilities": {
      "elicitation": {},                   // enables server-initiated requests
      "experimental": {
        "portico": {
          "listChanged": "live"            // opt into real-time push notifications
        }
      }
    },
    "clientInfo": { "name": "my-agent", "version": "2.0.0" }
  }
}

// 2. Gateway → client: initialize result
// Response header: Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "tools":     { "listChanged": true },
      "resources": { "listChanged": true },
      "prompts":   { "listChanged": true }
    },
    "serverInfo": {
      "name":        "portico-gateway",
      "version":     "phase-3.5",
      "description": "Portico — multi-tenant MCP gateway and Skill runtime"
    }
  }
}

// 3. Client → gateway: notifications/initialized (no ID = notification)
POST /mcp   [Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE]
{
  "jsonrpc": "2.0",
  "method": "notifications/initialized"
}
// → 202 Accepted

// 4. Client opens SSE channel
GET /mcp   [Accept: text/event-stream]  [Mcp-Session-Id: s_Ak3xVwLm9pQr7TcE]
// → 200 text/event-stream (long-lived)
```

---

## Related

- [MCP Gateway](/concepts/mcp-gateway) — the full gateway concept: how northbound and southbound are combined
- [MCP southbound](/concepts/mcp-southbound) — how Portico connects to downstream MCP servers
- [MCP registry](/concepts/mcp-registry) — registering and managing downstream servers
- [Approvals](/concepts/approvals) — the full approval and elicitation flow
- [Code Mode](/concepts/code-mode) — the Starlark sandbox activated via `initialize` experimental capabilities
- [Agent Profiles](/concepts/agent-profiles) — how per-consumer allowlists restrict the tool surface
- [Authentication](/concepts/authentication) — how JWT validation populates the session's tenant identity
