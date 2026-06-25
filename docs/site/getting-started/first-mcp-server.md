# Your first MCP server

This guide walks you through the complete lifecycle of a downstream MCP server in Portico: registering it, watching its tools appear in the aggregated catalog under a namespaced prefix, and calling a tool over the MCP northbound. The example uses `mockmcp`, the in-repo mock server included in `examples/servers/mock/`.

By the end you will have:

- A `mockmcp` process managed by the gateway supervisor
- Its four tools visible in `tools/list` as `mock.echo`, `mock.add`, `mock.slow`, and `mock.broken`
- A successful `tools/call` round-trip through `POST /mcp`

---

## Prerequisites

Build both binaries in one step:

```bash
make build mockmcp
```

`make build` produces `./bin/portico`. `make mockmcp` produces `./bin/mockmcp` from `examples/servers/mock/cmd/mockmcp/`. Both are CGo-free static binaries.

---

## Start Portico in dev mode

Dev mode disables JWT validation and binds only to localhost. It is the right starting point for local experimentation.

```bash
./bin/portico dev
```

The gateway is now listening on `127.0.0.1:8080`. The synthetic tenant is `dev` (configurable with `--tenant`). No authentication header is required for any request.

::: info Dev mode scope
Dev mode is intentionally narrow — it enforces the localhost-only bind so credentials never leave the machine, but it also skips policy, approval, and vault wiring. Use it only for local experimentation. See [Deployment](/guides/deployment) for a production configuration with JWT and a real vault.
:::

Confirm the server is healthy:

```bash
curl -s http://127.0.0.1:8080/healthz
```

Expected response: `{"status":"ok"}`.

---

## Register the mock server

Portico's server registry is the per-tenant record of downstream MCP servers the gateway manages. You populate it via the REST API or by declaring servers in the YAML config file before startup. Both paths end up in the same SQLite-backed registry.

### Option A — REST API

```bash
curl -s -X POST http://127.0.0.1:8080/v1/servers \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "mock",
    "display_name": "Mock MCP server",
    "transport": "stdio",
    "runtime_mode": "shared_global",
    "stdio": {
      "command": "./bin/mockmcp",
      "args": ["--name", "mock"]
    },
    "health": {
      "ping_interval": "30s",
      "ping_timeout": "5s",
      "startup_grace": "5s"
    }
  }'
```

A successful registration returns **HTTP 201** and the stored server spec:

```json
{
  "id": "mock",
  "display_name": "Mock MCP server",
  "transport": "stdio",
  "runtime_mode": "shared_global",
  "stdio": {
    "command": "./bin/mockmcp",
    "args": ["--name", "mock"],
    "start_timeout": "10s"
  },
  "health": {
    "ping_interval": "30s",
    "ping_timeout": "5s",
    "startup_grace": "5s"
  },
  "lifecycle": {
    "backoff_initial": "500ms",
    "backoff_max": "30s",
    "max_restart_attempts": 5,
    "circuit_open_duration": "5m0s",
    "shutdown_grace": "5s"
  },
  "enabled": true
}
```

Notice that the response fills in lifecycle and timeout defaults you did not supply. Those are applied at validation time by the registry.

**Verify the registration:**

```bash
curl -s http://127.0.0.1:8080/v1/servers/mock | jq .
```

### Option B — YAML config

For a deployment where servers are declared statically, add a `servers` block to `portico.yaml` and let the gateway seed the registry at startup:

```yaml
server:
  bind: 127.0.0.1:8080

# auth: omit entirely for dev mode (localhost bind required)

storage:
  driver: sqlite
  dsn: "file:./portico.db"

tenants:
  - id: dev
    display_name: Dev tenant

servers:
  - id: mock
    display_name: Mock MCP server
    transport: stdio
    runtime_mode: shared_global
    stdio:
      command: ./bin/mockmcp
      args: [--name, mock]
```

Then start with:

```bash
./bin/portico serve --config portico.yaml
```

The REST API still works for runtime additions once the server is running. The YAML config is idempotent — restarting with the same spec is safe.

### Server ID constraints

Server IDs must match `^[a-z0-9][a-z0-9_-]{0,31}$`. The ID becomes part of every tool name the gateway exposes, so pick something short and lowercase: `mock`, `github`, `linear-prod`, `pg-analytics`.

### Runtime modes

| Mode | Supervisor behavior |
|---|---|
| `shared_global` | One process shared across all sessions (default for stdio) |
| `per_tenant` | One process per tenant |
| `per_user` | One process per JWT subject |
| `per_session` | One process per MCP session (maximum isolation) |
| `remote_static` | No managed process; used for HTTP downstreams |

The mock server works well under `shared_global`. Use `per_session` when a downstream carries per-session state.

---

## Connect over the MCP northbound

Portico's northbound MCP transport accepts JSON-RPC 2.0 at `POST /mcp`. The same endpoint handles `GET /mcp` (SSE stream for server-initiated messages) and `DELETE /mcp` (session termination).

### Step 1 — Initialize

Every MCP client begins with an `initialize` request. Portico uses protocol version `2025-11-25`.

```http
POST /mcp HTTP/1.1
Host: 127.0.0.1:8080
Content-Type: application/json
Accept: application/json

{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {},
    "clientInfo": {
      "name": "my-agent",
      "version": "0.1.0"
    }
  }
}
```

The response includes the gateway's aggregate capabilities and a session identifier:

```http
HTTP/1.1 200 OK
Content-Type: application/json
Mcp-Session-Id: <session-id>

{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-11-25",
    "capabilities": {
      "tools": {}
    },
    "serverInfo": {
      "name": "portico-gateway",
      "version": "phase-3.5",
      "description": "Portico — multi-tenant MCP gateway and Skill runtime"
    }
  }
}
```

**Capture the `Mcp-Session-Id` response header.** Every subsequent request to the gateway must carry it:

```
Mcp-Session-Id: <session-id>
```

Without the header, subsequent requests return HTTP 404 with a JSON-RPC error pointing you back to `initialize`.

Using curl, the full sequence looks like:

```bash
# Initialize and capture the session ID
SESSION=$(curl -s -D - -o /dev/null -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc":"2.0","id":1,"method":"initialize",
    "params":{
      "protocolVersion":"2025-11-25",
      "capabilities":{},
      "clientInfo":{"name":"my-agent","version":"0.1.0"}
    }
  }' | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/{gsub(/\r/,"",$2);print $2;exit}')

echo "Session: $SESSION"
```

### Step 2 — Send the initialized notification

The MCP protocol requires the client to send a `notifications/initialized` notification after receiving the initialize result. This is a one-way message with no response:

```bash
curl -s -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
```

---

## List aggregated tools

With the session established, call `tools/list` to see every tool across all registered downstream servers. Portico fans this request out to every enabled server visible to your tenant, collects the results, and prefixes each tool name with the server's ID.

```bash
curl -s -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | jq .
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "mock.add",
        "description": "Add two integers",
        "inputSchema": {
          "type": "object",
          "properties": {
            "a": {"type": "integer"},
            "b": {"type": "integer"}
          },
          "required": ["a", "b"]
        }
      },
      {
        "name": "mock.broken",
        "description": "Always returns an error",
        "inputSchema": {"type": "object"}
      },
      {
        "name": "mock.echo",
        "description": "Echo a message back",
        "inputSchema": {
          "type": "object",
          "properties": {
            "message": {"type": "string"}
          },
          "required": ["message"]
        }
      },
      {
        "name": "mock.slow",
        "description": "Simulates a slow tool that emits progress notifications",
        "inputSchema": {
          "type": "object",
          "properties": {
            "duration_ms": {"type": "integer", "minimum": 1}
          }
        }
      }
    ]
  }
}
```

The four tools the mock server advertises (`echo`, `add`, `slow`, `broken`) appear as `mock.echo`, `mock.add`, `mock.slow`, and `mock.broken`. The separator is a dot and the prefix is exactly the server `id` you registered. The tools are sorted alphabetically.

### How namespacing works

When the gateway aggregates `tools/list`, for each upstream tool it applies:

```
namespaced_name = server_id + "." + tool_name
```

When routing a `tools/call`, the dispatcher splits on the **first** dot:

- `mock.echo` → server `mock`, tool `echo`
- `github.search_repos` → server `github`, tool `search_repos`
- `my-server.ns.sub_tool` → server `my-server`, tool `ns.sub_tool`

A tool name that contains no dot is rejected with JSON-RPC error code `-32004` (`tool name must be qualified as <server>.<tool>`). Always use the full qualified form when calling.

---

## Call a tool

Invoke `mock.echo` by sending a `tools/call` request with the fully-qualified name:

```bash
curl -s -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "mock.echo",
      "arguments": {"message": "hello from Portico"}
    }
  }' | jq .
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "hello from Portico"
      }
    ]
  }
}
```

Try `mock.add`:

```bash
curl -s -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
      "name": "mock.add",
      "arguments": {"a": 17, "b": 25}
    }
  }' | jq .result.content[0].text
```

Output: `"42"`

---

## Error behavior

### Calling a non-existent tool

A tool name that Portico cannot route returns an error at the JSON-RPC layer — the HTTP status is still **200**, but the response carries an error object:

```bash
curl -s -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc":"2.0","id":5,
    "method":"tools/call",
    "params":{"name":"mock.does_not_exist","arguments":{}}
  }' | jq .error
```

Response:

```json
{
  "code": -32004,
  "message": "unknown tool",
  "data": {"name": "mock.does_not_exist"}
}
```

Error code `-32004` (`ErrToolNotEnabled`) means the tool name resolved to a valid server but the tool itself was not found in that server's catalog.

### Calling an upstream that always errors

`mock.broken` is intentionally broken — the mock server always returns a JSON-RPC internal error for it:

```bash
curl -s -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H "Mcp-Session-Id: $SESSION" \
  -d '{
    "jsonrpc":"2.0","id":6,
    "method":"tools/call",
    "params":{"name":"mock.broken","arguments":{}}
  }' | jq .error
```

```json
{
  "code": -32603,
  "message": "intentional failure"
}
```

The gateway propagates the downstream error as-is. Nothing in the northbound HTTP path changes a downstream MCP error into an HTTP non-200 status.

---

## What just happened

The sequence above exercised several Portico subsystems:

1. **Registry.** `POST /v1/servers` validated the spec (ID format, transport/mode consistency, defaults), persisted it, and published a `ChangeAdded` event to the supervisor.

2. **Supervisor.** The supervisor consumed the event and started a `mockmcp` process under `shared_global` mode, running the `initialize` → `tools/list` handshake to confirm the downstream is healthy. The process is now pooled and reused across sessions.

3. **Northbound transport.** `POST /mcp` accepted the `initialize` request, allocated a session, and returned the `Mcp-Session-Id` header. The session is pinned to the `dev` tenant.

4. **Dispatcher.** `tools/list` fanned out to every enabled server for the tenant (one server, `mock`), applied the `{server_id}.{tool_name}` namespace prefix, sorted the result, and cached it for 60 seconds (per session). Subsequent `tools/list` calls within the TTL are answered from the cache without touching the subprocess.

5. **Tool call routing.** `tools/call` for `mock.echo` split the name on the first dot to resolve `serverID=mock`, acquired the pooled client for that server, forwarded the call (with the original tool name `echo` — the prefix is stripped before the downstream sees it), and proxied the response back.

---

## Managing the server

Once registered, you can control the server lifecycle at runtime:

```bash
# Check health
curl -s http://127.0.0.1:8080/api/servers/mock/health | jq .

# Disable the server (tools become unreachable, process stops)
curl -s -X POST http://127.0.0.1:8080/v1/servers/mock/disable

# Re-enable
curl -s -X POST http://127.0.0.1:8080/v1/servers/mock/enable

# Hot-reload config (re-reads the spec, triggers supervisor restart)
curl -s -X POST http://127.0.0.1:8080/v1/servers/mock/reload

# List running instances (useful under per_tenant or per_session modes)
curl -s http://127.0.0.1:8080/v1/servers/mock/instances | jq .

# Remove the registration entirely
curl -s -X DELETE http://127.0.0.1:8080/v1/servers/mock
```

Status transitions (`unknown` → `starting` → `healthy` / `unhealthy` / `circuit_open`) are tracked by the supervisor and visible on the health endpoint.

---

## Registering an HTTP downstream

For an MCP server that speaks HTTP rather than stdio — for example a remotely-managed service — use `transport: http` with `runtime_mode: remote_static`:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/servers \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "remote-svc",
    "display_name": "Remote MCP service",
    "transport": "http",
    "runtime_mode": "remote_static",
    "http": {
      "url": "https://mcp.example.internal",
      "timeout": "30s"
    }
  }'
```

Portico does not spawn a process for `remote_static` servers. The supervisor polls the endpoint with `ping` requests on the configured interval to determine health. Credential injection (Bearer tokens, OAuth exchange) is handled separately by the vault and credential injectors — see [Credentials vault](/concepts/credentials-vault).

---

## Related

- [MCP gateway concept](/concepts/mcp-gateway) — how the gateway handles the full northbound-to-southbound path
- [MCP northbound](/concepts/mcp-northbound) — transport details, session lifecycle, SSE, cancellation
- [MCP southbound](/concepts/mcp-southbound) — how Portico connects to and manages downstream servers
- [Register an MCP server guide](/guides/register-mcp-server) — production registration, env injection, auth specs
- [Catalog and sessions](/concepts/catalog-and-sessions) — snapshot-based stable catalogs, drift detection
- [Agent profiles](/concepts/agent-profiles) — controlling which tools a given agent is allowed to call
- [Dev mode walkthrough](/getting-started/dev-mode) — full dev-mode feature tour
