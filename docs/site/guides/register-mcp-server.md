# Register an MCP server

Portico is a gateway in front of many downstream MCP servers. Adding a server
means giving Portico a spec — transport, command or URL, runtime isolation mode,
health and lifecycle knobs — and Portico takes it from there: spawning and
supervising processes, namespacing tools, enforcing policy, and routing calls.

This guide covers two registration paths: static configuration through
`portico.yaml` and dynamic registration through the REST API. Both produce the
same persistent record in the server registry and trigger the same lifecycle
machinery.

::: info Prerequisites
A running Portico instance. In development, `./bin/portico dev` starts a
server on `127.0.0.1:8080` with a synthetic tenant and no JWT requirement.
See [Dev mode](/getting-started/dev-mode) for setup.
:::

---

## How registration works

When a server spec reaches the registry — whether from config file or API — the
following happens:

1. The spec is validated. Validation checks transport type, id format
   (`^[a-z0-9][a-z0-9_-]{0,31}$`), required sub-fields per transport, and
   runtime-mode constraints. Validation errors name the offending field.
2. The canonical record is persisted to SQLite (tenant-scoped).
3. A `ChangeAdded` or `ChangeUpdated` event is broadcast to the process
   supervisor, which acts on it according to the server's `runtime_mode`.
4. For `stdio` servers the supervisor lazily spawns the downstream process on
   the first `tools/call` in that tenant/user/session scope.
5. Northbound MCP clients receive all tools prefixed with the server id:
   `{server_id}.{tool_name}`.

See [MCP server registry](/concepts/mcp-registry) and
[MCP southbound](/concepts/mcp-southbound) for the architecture behind these
steps.

---

## Static configuration — portico.yaml

The `servers:` block declares servers that Portico registers at boot and
re-registers on hot-reload when the file changes.

### stdio transport

A stdio server is a local process. Portico spawns it, wires stdin/stdout as
the JSON-RPC channel, and captures stderr to a rotating log file.

```yaml
servers:
  - id: github
    display_name: GitHub
    transport: stdio
    runtime_mode: per_user
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        - "GITHUB_TOKEN={{secret:github_token}}"
      cwd: ""
      start_timeout: 15s
    health:
      ping_interval: 30s
      ping_timeout: 5s
      startup_grace: 10s
    lifecycle:
      idle_timeout: 600s
      backoff_initial: 500ms
      backoff_max: 30s
      max_restart_attempts: 5
      circuit_open_duration: 5m
      shutdown_grace: 5s
    limits:
      memory_max: 512MB
      open_files: 256
```

**Key fields:**

| Field | Required | Default | Notes |
|-------|----------|---------|-------|
| `id` | yes | — | Lowercase alphanumeric, underscores, hyphens; max 32 chars |
| `transport` | yes | — | `stdio` or `http` |
| `runtime_mode` | no | `shared_global` | See [Runtime modes](#runtime-modes) below |
| `stdio.command` | yes (stdio) | — | Executable; never use shell form |
| `stdio.args` | no | `[]` | Argv list passed to the command |
| `stdio.env` | no | `[]` | `KEY=VALUE` pairs; <span v-pre>`{{secret:name}}`</span> resolves from vault |
| `stdio.cwd` | no | inherited | Working directory for the spawned process |
| `stdio.start_timeout` | no | `10s` | Budget for the MCP `initialize` round-trip |
| `health.ping_interval` | no | disabled | `0` disables the periodic liveness probe |
| `health.ping_timeout` | no | `5s` | Timeout per `ping` call |
| `health.startup_grace` | no | `5s` | Time allowed before health checks begin |
| `lifecycle.idle_timeout` | no | disabled | Kill after N seconds of no `tools/call` |
| `lifecycle.backoff_initial` | no | `500ms` | First restart delay |
| `lifecycle.backoff_max` | no | `30s` | Ceiling on exponential backoff |
| `lifecycle.max_restart_attempts` | no | `5` | `0` = unlimited |
| `lifecycle.circuit_open_duration` | no | `5m` | Lock-out window after max attempts reached |
| `lifecycle.shutdown_grace` | no | `5s` | SIGTERM → SIGKILL window |
| `limits.memory_max` | no | none | e.g. `256MB`; applied via OS resource limits |
| `limits.cpu_millicores` | no | none | Linux only; ignored elsewhere |
| `limits.open_files` | no | none | `RLIMIT_NOFILE` |
| `limits.processes` | no | none | `RLIMIT_NPROC` |

::: warning Command form
Always supply `command` + `args` as separate fields. Portico executes the
command directly — never via a shell. A spec with `command: "sh -c npx ..."` is
a security violation and is blocked by the supervisor.
:::

### http transport

An HTTP server is a remotely-hosted MCP endpoint. Portico acts as a stateless
client; no process is managed.

```yaml
servers:
  - id: filesystem-remote
    display_name: Remote Filesystem
    transport: http
    runtime_mode: remote_static
    http:
      url: "https://mcp.internal.example.com/filesystem"
      auth_header: ""    # Phase 5: populated from vault via auth.secret_ref
      timeout: 30s
    health:
      ping_interval: 60s
      ping_timeout: 5s
```

`runtime_mode` must be `remote_static` for HTTP servers; any other value is
rejected at validation time. Portico does not spawn processes for HTTP servers —
the supervisor holds only a status record.

### Secret references in env

Env values in `stdio.env` may include <span v-pre>`{{secret:name}}`</span> placeholders. These
are resolved from Portico's credential vault immediately before the process is
spawned. If a placeholder cannot be resolved, the server fails to start with a
clear error message rather than launching with a literal placeholder string.

```yaml
stdio:
  env:
    - "GITHUB_TOKEN={{secret:github_token}}"
    - "BASE_URL={{env:UPSTREAM_URL}}"
```

- <span v-pre>`{{secret:name}}`</span> — vault lookup by name, scoped to the tenant.
- <span v-pre>`{{env:VAR}}`</span> — passes through a host-process environment variable.

See [Credentials vault](/concepts/credentials-vault) for how to populate vault
entries.

---

## Runtime modes

`runtime_mode` controls how many process instances Portico maintains and how
they are keyed.

| Mode | stdio | http | Instance key |
|------|-------|------|--------------|
| `shared_global` | yes | no | One process shared across all tenants and users |
| `per_tenant` | yes | no | One process per tenant |
| `per_user` | yes | no | One process per JWT subject within a tenant |
| `per_session` | yes | no | One process per MCP session |
| `remote_static` | no | yes | No process; one HTTP client per gateway instance |

In `per_tenant` and `per_user` modes two tenants (or two users within a tenant)
using the same server id each get a completely independent process with its own
stdin/stdout pair. There is no shared state between them.

In `per_session` mode the process is torn down when the northbound MCP session
closes, making it the highest-isolation option at the cost of cold-start latency
on every session.

The `shared_global` default is appropriate for stateless tools and tools that
explicitly manage their own multi-tenant state. Use `per_tenant` or `per_user`
for servers that carry user-specific credentials or internal state.

---

## Credentials and auth

The optional `auth` block on a server spec names a credential injection
strategy. Phase 5 fully implements these; the spec is parsed and validated from
Phase 2 onward.

```yaml
servers:
  - id: linear
    transport: stdio
    stdio:
      command: npx
      args: ["-y", "@linear/mcp-server"]
    auth:
      strategy: env_inject
      default_risk_class: write
      env:
        - "LINEAR_API_KEY={{secret:linear_api_key}}"
```

Available strategies:

| Strategy | Transport | Description |
|----------|-----------|-------------|
| `env_inject` | stdio | Injects resolved secrets as environment variables |
| `http_header_inject` | http | Injects resolved values as HTTP request headers |
| `secret_reference` | http | Single secret resolved to `Authorization: Bearer <value>` |
| `oauth2_token_exchange` | http | RFC 8693 token exchange before each downstream call |

`default_risk_class` sets the approval risk level for any tool this server
exposes that does not have a skill-level override. Accepted values: `read`,
`write`, `sensitive_read`, `external_side_effect`, `destructive`.

---

## Dynamic registration — REST API

Every CRUD operation available in `portico.yaml` is also available over HTTP.
The REST API is tenant-scoped: the caller's JWT (or dev-mode synthetic tenant)
determines which tenant's registry is affected.

### Create a server

```bash
curl -s -X POST http://localhost:8080/v1/servers \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "github",
    "display_name": "GitHub",
    "transport": "stdio",
    "runtime_mode": "per_user",
    "stdio": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": ["GITHUB_TOKEN={{secret:github_token}}"],
      "start_timeout": "15s"
    },
    "health": {
      "ping_interval": "30s",
      "ping_timeout": "5s"
    },
    "lifecycle": {
      "idle_timeout": "600s",
      "max_restart_attempts": 5
    }
  }'
```

A successful create returns `201 Created` with the full server record:

```json
{
  "id": "github",
  "tenant_id": "dev",
  "display_name": "GitHub",
  "transport": "stdio",
  "runtime_mode": "per_user",
  "status": "unknown",
  "status_detail": "",
  "schema_hash": "",
  "last_error": "",
  "enabled": true,
  "created_at": "2026-06-25T10:00:00Z",
  "updated_at": "2026-06-25T10:00:00Z",
  "spec": { "...": "full spec" }
}
```

`POST /v1/servers` to an existing `id` returns `200 OK` (upsert semantics).
`PUT /v1/servers/{id}` requires the server to exist and returns 404 otherwise.

::: tip Console
`POST /api/servers` mirrors `POST /v1/servers` and is the path the Console uses.
Both endpoints are tenant-scoped and accept identical JSON bodies.
:::

### Create an HTTP server

```bash
curl -s -X POST http://localhost:8080/v1/servers \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "filesystem-remote",
    "transport": "http",
    "runtime_mode": "remote_static",
    "http": {
      "url": "https://mcp.internal.example.com/filesystem",
      "timeout": "30s"
    }
  }'
```

### Full endpoint reference

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/servers` | List all servers for the caller's tenant |
| `POST` | `/v1/servers` | Create or upsert a server; returns 201 on new id |
| `GET` | `/v1/servers/{id}` | Fetch a single server including live instance list |
| `PUT` | `/v1/servers/{id}` | Replace spec; 404 if not found |
| `DELETE` | `/v1/servers/{id}` | Stop all instances, remove record; returns 204 |
| `POST` | `/v1/servers/{id}/reload` | Drain and restart all instances; returns 202 |
| `POST` | `/v1/servers/{id}/enable` | Set `enabled=true`; instances start on next call |
| `POST` | `/v1/servers/{id}/disable` | Set `enabled=false`; stop all instances |
| `GET` | `/v1/servers/{id}/instances` | List running instances for this server |
| `POST` | `/api/servers/{id}/restart` | Restart with an optional `{"reason":"..."}` body |
| `GET` | `/api/servers/{id}/logs` | SSE stream of stdout + stderr lines |
| `GET` | `/api/servers/{id}/health` | Current status snapshot |
| `PATCH` | `/api/servers/{id}` | Partial update: `enabled`, `env_overrides` |

All paths are under the authenticated route group. In dev mode (`portico dev`)
the synthetic tenant is injected automatically; in production a valid Bearer JWT
is required.

---

## Verifying tool namespacing

After registration, Portico prefixes every tool this server exposes with the
server id followed by a dot. A server with `id: github` that advertises a
`create_issue` tool appears to northbound MCP clients as `github.create_issue`.

To confirm tools are visible, call `tools/list` over the MCP endpoint:

```bash
curl -s -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
  }'
```

The response will include entries like:

```json
{
  "tools": [
    {
      "name": "github.create_issue",
      "description": "Create a new GitHub issue",
      "inputSchema": { "...": "..." }
    },
    {
      "name": "github.list_pull_requests",
      "description": "...",
      "inputSchema": { "...": "..." }
    }
  ]
}
```

The separator is always `.` (a single dot). Server ids may contain underscores
and hyphens but not dots, so the first `.` in any qualified tool name
unambiguously separates the server id from the tool name. Tool names that
contain dots themselves are preserved as-is after the first separator.

If a newly registered server's tools do not appear:

1. Check the server status — `GET /v1/servers/{id}` returns `status` and
   `last_error`.
2. Confirm the server is `enabled: true`.
3. For stdio servers, the process starts lazily on first call, not on
   registration. Make a call to any tool the server is expected to expose and
   then re-check `tools/list`.

---

## Watching server health

### Status field

Every server record carries a `status` field maintained by the supervisor:

| Value | Meaning |
|-------|---------|
| `unknown` | Registered but never started |
| `starting` | Process is spawning |
| `healthy` | MCP `ping` succeeds within `health.ping_timeout` |
| `unhealthy` | Recent ping failure; process still running |
| `disabled` | Operator-disabled via `POST /{id}/disable` |
| `circuit_open` | Max restart attempts exhausted; calls blocked for `circuit_open_duration` |

### Health endpoint

```bash
curl -s http://localhost:8080/api/servers/github/health
```

```json
{
  "server_id": "github",
  "status": "healthy",
  "status_detail": "",
  "enabled": true,
  "last_error": "",
  "updated_at": "2026-06-25T10:05:23Z"
}
```

### Instance list

```bash
curl -s http://localhost:8080/v1/servers/github/instances
```

Each instance record includes its process id, start time, last call time,
restart count, and current state. For `per_tenant` and `per_user` modes, multiple
rows may be present simultaneously — one per active tenant or user.

---

## Live log streaming

`GET /api/servers/{id}/logs` returns an SSE stream. Each event carries the
process output line, the originating stream (`stdout` or `stderr`), and a
nanosecond timestamp:

```bash
curl -s http://localhost:8080/api/servers/github/logs
```

```
: connected

event: log
data: {"at":"2026-06-25T10:05:23.491Z","stream":"stderr","text":"GitHub MCP server v1.2.0 started"}

event: log
data: {"at":"2026-06-25T10:05:24.102Z","stream":"stderr","text":"Listening on stdio"}
```

Pass `?since=<RFC3339Nano>` to filter historical lines:

```bash
curl -s 'http://localhost:8080/api/servers/github/logs?since=2026-06-25T10:00:00.000Z'
```

The stream emits a `: keep-alive` comment every 15 seconds so reverse proxies
do not close idle connections. The stream closes when the server-side context
is cancelled (client disconnect or server shutdown).

---

## Reloading and restarting

### Reload (drain + restart)

`POST /v1/servers/{id}/reload` tells the supervisor to gracefully close every
running instance for this server and start fresh on the next call. It does not
change the spec. The endpoint returns `202 Accepted` immediately; the drain
happens asynchronously.

```bash
curl -s -X POST http://localhost:8080/v1/servers/github/reload
```

```json
{"status":"reloading","id":"github"}
```

### Restart via Console API

`POST /api/servers/{id}/restart` accepts an optional reason and is the path the
Console uses. It records the restart event to the audit log alongside the actor
and reason:

```bash
curl -s -X POST http://localhost:8080/api/servers/github/restart \
  -H 'Content-Type: application/json' \
  -d '{"reason": "credential rotation"}'
```

### Clearing a circuit-open state

When a server enters `circuit_open`, operator-triggered reload or restart clears
the circuit immediately and attempts a fresh start rather than waiting for
`circuit_open_duration` to expire.

---

## Enabling and disabling

Disabling a server stops all running instances without removing the spec from
the registry:

```bash
curl -s -X POST http://localhost:8080/v1/servers/github/disable
```

Re-enabling it marks the server as `enabled=true`; the supervisor will start
an instance the next time a call arrives:

```bash
curl -s -X POST http://localhost:8080/v1/servers/github/enable
```

The Console's Servers list page exposes enable/disable as a toggle on each row
without a page reload.

---

## Hot-reload from portico.yaml

When `portico.yaml` is modified while Portico is running, the config watcher
detects the change and diffs the `servers:` block. Servers added to the file
are registered; servers removed are deleted (instances stopped); servers with
changed specs are updated and their instances drained and restarted. Changes
apply within 500 ms of the file-write debounce. Requests in flight are not
dropped — the supervisor waits for in-flight calls to complete before shutting
down an affected instance.

---

## Deleting a server

```bash
curl -s -X DELETE http://localhost:8080/v1/servers/github
```

Returns `204 No Content`. The supervisor stops all running instances before
the record is removed. Namespaced tools advertised by this server disappear from
subsequent `tools/list` responses once all running sessions have completed or
reconnected.

---

## Related

- [MCP server registry](/concepts/mcp-registry) — architecture of the registry
  store, change events, and supervisor integration.
- [MCP southbound](/concepts/mcp-southbound) — how Portico connects to stdio
  and HTTP downstream servers.
- [Your first MCP server](/getting-started/first-mcp-server) — end-to-end
  walkthrough: write a mock server, register it, and call a tool.
- [Credentials vault](/concepts/credentials-vault) — storing and referencing
  secrets for <span v-pre>`{{secret:name}}`</span> interpolation.
- [Policy](/concepts/policy) — approval flows and risk classification per server
  or tool.
- [Agent profiles](/concepts/agent-profiles) — restricting which servers a
  consumer can reach via `allowed_mcp_servers`.
- [REST API reference](/reference/rest-api) — full OpenAPI surface including all
  `/v1/servers` and `/api/servers` endpoints.
