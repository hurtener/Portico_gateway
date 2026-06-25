# Server registry

The server registry is Portico's per-tenant, runtime-mutable catalog of downstream MCP servers. Operators register servers via the REST API or seed them from `portico.yaml`; from that point on every aspect — command, environment, transport, runtime mode, health parameters — can be changed without restarting the binary. The registry owns the persistent `ServerSpec` row, exposes a change-event bus that the process supervisor subscribes to, and feeds the northbound dispatcher with the current view of what each tenant can reach.

## How it works

When an operator calls `POST /v1/servers`, the registry validates the spec, writes it to SQLite, then publishes a `ChangeAdded` event on an internal fan-out channel. The process supervisor is a subscriber; on receiving the event it spawns (or routes) the downstream process according to the spec's runtime mode. Every subsequent `PUT` or `POST /api/servers/{id}/restart` publishes a `ChangeUpdated` event: the supervisor drains in-flight connections within the configured shutdown-grace window and restarts the affected instances. `DELETE` publishes `ChangeRemoved`; the supervisor terminates all instances and removes the entry from its routing table.

The northbound MCP dispatcher reads the registry via the southbound manager on every `tools/call` — it never caches a stale process handle between tool calls. This means that a spec update applied during an active session takes effect on the next call within that session, without the session being torn down.

```
  REST handler
      │  Upsert / Delete
      ▼
  Registry (SQLite + in-memory)
      │  ChangeEvent (Added / Updated / Removed)
      ▼
  Process Supervisor ──► spawn / drain / kill subprocess
      │
      ▼
  Southbound Client ──► tools/call  ──► northbound dispatcher
```

## Server spec

Every registered server is described by a `ServerSpec`. The fields below are the canonical source of truth; the same struct is accepted as JSON by the REST API and as YAML in `portico.yaml`.

```yaml
id: github                      # required; matches ^[a-z0-9][a-z0-9_-]{0,31}$
display_name: "GitHub MCP"      # optional; defaults to id
transport: stdio                # required; "stdio" or "http"
runtime_mode: per_user          # see Runtime modes below

stdio:                          # required when transport=stdio
  command: npx
  args: ["-y", "@modelcontextprotocol/server-github"]
  env:
    - "GITHUB_TOKEN={{secret:github_token}}"   # vault reference
    - "LOG_LEVEL=info"
  cwd: ""
  start_timeout: "15s"

health:
  ping_interval: "30s"    # 0 disables periodic probe
  ping_timeout: "5s"
  startup_grace: "10s"

lifecycle:
  idle_timeout: "600s"          # 0 disables; shuts down after inactivity
  backoff_initial: "500ms"
  backoff_max: "30s"
  max_restart_attempts: 5       # 0 = unlimited
  circuit_open_duration: "5m"
  shutdown_grace: "5s"

limits:
  memory_max: "512MB"
  cpu_millicores: 500           # Linux only
  open_files: 256
  processes: 64

enabled: true

auth:
  strategy: env_inject          # see Credential injection below
  default_risk_class: write
```

For an HTTP downstream (a remote MCP server Portico does not manage as a process):

```yaml
id: remote-search
transport: http
runtime_mode: remote_static     # the only valid mode for http transport

http:
  url: "https://mcp.internal/search"
  auth_header: "Authorization"  # header name; value from vault via auth.secret_ref
  timeout: "30s"

auth:
  strategy: secret_reference
  secret_ref: "search_api_key"
```

### Spec field reference

| Field | Type | Default | Description |
|---|---|---|---|
| `id` | string | — | Unique within a tenant. Lowercase alphanumeric, hyphens, underscores; 1–32 chars. |
| `display_name` | string | equals `id` | Human-readable label shown in the Console. |
| `transport` | `stdio` \| `http` | — | How Portico connects to the server. |
| `runtime_mode` | string | `shared_global` (stdio) / `remote_static` (http) | Process isolation boundary. See below. |
| `stdio.command` | string | — | Executable path or name on `PATH`. Required for stdio transport. |
| `stdio.args` | []string | `[]` | Command-line arguments. |
| `stdio.env` | []string | `[]` | `KEY=VALUE` pairs. Supports <span v-pre>`{{secret:name}}`</span> and <span v-pre>`{{env:VAR}}`</span> placeholders. |
| `stdio.cwd` | string | process cwd | Working directory for the subprocess. |
| `stdio.start_timeout` | duration | `10s` | Time allowed for the process to complete `initialize` handshake. |
| `http.url` | string | — | Base URL of the remote MCP endpoint. Required for http transport. |
| `http.auth_header` | string | — | Header name to carry the resolved credential. |
| `http.timeout` | duration | `30s` | Per-request timeout for southbound HTTP calls. |
| `health.ping_interval` | duration | `0` (disabled) | How often to probe with MCP `ping`. |
| `health.ping_timeout` | duration | `5s` | Timeout per probe. |
| `health.startup_grace` | duration | `5s` | Grace window after spawn before probes start. |
| `lifecycle.idle_timeout` | duration | `0` (disabled) | Shuts down after this long with no tool calls. |
| `lifecycle.backoff_initial` | duration | `500ms` | First retry delay after a crash. |
| `lifecycle.backoff_max` | duration | `30s` | Maximum retry delay. |
| `lifecycle.max_restart_attempts` | int | `5` | Consecutive failures before entering `circuit_open`. |
| `lifecycle.circuit_open_duration` | duration | `5m` | How long `circuit_open` lasts before another start is tried. |
| `lifecycle.shutdown_grace` | duration | `5s` | SIGTERM-to-SIGKILL window on restart/delete. |
| `limits.memory_max` | string | — | e.g. `"256MB"`. Applied as `RLIMIT_AS` (Linux/Darwin). |
| `limits.cpu_millicores` | int | — | Linux cgroups `cpu.max` when the `cgroups` build tag is set. |
| `limits.open_files` | int | — | `RLIMIT_NOFILE`. |
| `limits.processes` | int | — | `RLIMIT_NPROC`. |
| `enabled` | bool | `true` | `false` stops all instances and prevents new ones. |

Duration values accept human strings (`"5s"`, `"1m30s"`) or bare integers (treated as seconds).

## Runtime modes

The `runtime_mode` field controls the process isolation boundary. Each distinct key in the matrix below produces a separate subprocess.

| Mode | Isolation key | Typical use |
|---|---|---|
| `shared_global` | server ID only | Stateless tools safe to share across all tenants and users. |
| `per_tenant` | server ID + tenant ID | Tools that hold per-tenant state or credentials. |
| `per_user` | server ID + tenant ID + JWT `sub` | User-scoped tokens, e.g. OAuth access tokens per operator. |
| `per_session` | server ID + tenant ID + user + session ID | Maximum isolation; each MCP session gets its own process. |
| `remote_static` | server ID only (no process) | HTTP downstreams Portico routes to but does not spawn. |

`http` transport requires `remote_static`. All other modes require `stdio`.

## Server status lifecycle

The registry surfaces a `status` field on every server record, written by the supervisor:

| Status | Meaning |
|---|---|
| `unknown` | Newly registered; no start has been attempted yet. |
| `starting` | Subprocess spawned; waiting for the `initialize` handshake. |
| `healthy` | Running and passing health probes. |
| `unhealthy` | Probe failures detected; still attempting to serve calls. |
| `disabled` | `enabled=false`; no instances are running or will be started. |
| `circuit_open` | Maximum restart attempts exhausted; calls fail with `upstream_unavailable` until the circuit-open duration elapses. |

A manual restart via `POST /api/servers/{id}/restart` clears `circuit_open` and triggers a fresh start immediately.

## REST API

Two prefix families expose the same underlying registry. `/v1/servers` is the original surface retained for back-compatibility. `/api/servers` is the Console-facing surface that adds Phase 9 operations (partial update, restart, live log stream, health endpoint, activity log).

### `/v1/servers` — core CRUD

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/servers` | List all servers for the caller's tenant. |
| `POST` | `/v1/servers` | Create a server (returns 201 on new ID, 200 if ID already exists). |
| `GET` | `/v1/servers/{id}` | Get a single server, including live instance list. |
| `PUT` | `/v1/servers/{id}` | Full replacement of the spec. Body `id` must match path. |
| `DELETE` | `/v1/servers/{id}` | Remove the server; terminates running instances. Returns 204. |
| `POST` | `/v1/servers/{id}/reload` | Drain and restart all instances for this server. Returns 202. |
| `POST` | `/v1/servers/{id}/enable` | Set `enabled=true`; instances start lazily. |
| `POST` | `/v1/servers/{id}/disable` | Set `enabled=false`; running instances are stopped. |
| `GET` | `/v1/servers/{id}/instances` | List running instance records (PID, state, restart count, last call time). |

### `/api/servers` — Console surface

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/servers` | Same as `/v1/servers` plus substrate fields (capabilities counts, skills count, policy state, auth state). |
| `POST` | `/api/servers` | Create using the `Apply(MutOpCreate)` path; rejects duplicate IDs with 400. |
| `GET` | `/api/servers/{id}` | Server detail with substrate fields. |
| `PUT` | `/api/servers/{id}` | Full spec replacement. |
| `PATCH` | `/api/servers/{id}` | Partial update. Accepted fields: `enabled` (bool), `env_overrides` (map of key→value), `reason` (string). |
| `DELETE` | `/api/servers/{id}` | Remove the server. |
| `POST` | `/api/servers/{id}/restart` | Body: `{"reason": "..."}`. Emits a `ChangeUpdated` event; supervisor drains and restarts. Returns 202. |
| `GET` | `/api/servers/{id}/logs` | Server-sent events stream of stdout/stderr. See below. |
| `GET` | `/api/servers/{id}/health` | Supervisor's last-known status fields. |
| `GET` | `/api/servers/{id}/activity` | Audit-backed change history for this server. |

All endpoints are tenant-scoped via the JWT middleware. The `tenant_id` in every response body reflects the authenticated tenant; cross-tenant reads require the `admin` JWT scope.

### Error shape

Validation failures from `ServerSpec.Validate()` produce a structured 400:

```json
{
  "error": "invalid_spec",
  "message": "stdio.command: is required when transport=stdio",
  "detail": { "field": "stdio.command" }
}
```

## Log streaming

`GET /api/servers/{id}/logs` opens a Server-Sent Events stream of the subprocess's combined stdout/stderr. Each event carries a JSON payload:

```
event: log
data: {"at":"2026-06-25T10:15:03.123Z","stream":"stderr","text":"[info] tools: 14 loaded"}
```

The stream emits a keep-alive comment every 15 seconds so reverse proxies do not close idle connections:

```
: keep-alive
```

Use the optional `since` query parameter to replay historical lines from the supervisor's ring buffer before tailing live output:

```
GET /api/servers/{id}/logs?since=2026-06-25T10:00:00.000Z
```

The Console's server detail page subscribes to this stream while the page is mounted. Reconnect is handled automatically by the browser's `EventSource` API.

## List response shape

`GET /v1/servers` and `GET /api/servers` return an array. Each element includes the flattened record fields plus the full `spec` sub-object:

```jsonc
{
  "id": "github",
  "tenant_id": "acme",
  "display_name": "GitHub MCP",
  "transport": "stdio",
  "runtime_mode": "per_user",
  "status": "healthy",
  "status_detail": "",
  "schema_hash": "sha256:a1b2c3...",
  "last_error": "",
  "enabled": true,
  "created_at": "2026-06-01T09:00:00Z",
  "updated_at": "2026-06-25T10:00:00Z",
  "spec": { /* full ServerSpec */ },

  // /api/servers only — substrate enrichment
  "capabilities": { "tools": 14, "resources": 0, "prompts": 0, "apps": 0 },
  "skills_count": 3,
  "policy_state": "enforced",   // "none" | "enforced" | "approval" | "disabled"
  "auth_state": "env",          // "none" | "env" | "header" | "oauth" | "vault_ref"
  "last_seen": "2026-06-25T10:00:00Z"
}
```

`capabilities` counts come from the most recent catalog snapshot for the tenant; they are zero until the first session has run a `tools/list` cycle.

## Hot reconfiguration

No binary restart is required for any registry mutation. The change propagation path is:

1. REST handler calls `Registry.Upsert` or `Registry.Delete`.
2. Registry persists to SQLite, then calls `publish(ChangeEvent{...})`.
3. The supervisor's subscription channel receives the event; it drains active connections within `lifecycle.shutdown_grace` and spawns new instances from the updated spec.
4. The northbound MCP dispatcher resolves the new client on the next `tools/call`.

For YAML-driven deployments, the config watcher converts file changes to `Upsert`/`Delete` calls on the same registry path, so the propagation is identical.

::: tip
A `POST /api/servers/{id}/restart` is the surgical alternative to a full spec update when you want to recycle instances without changing configuration — for example, to pick up a rotated credential that is resolved at startup.
:::

## Credential injection

The `auth` block on a `ServerSpec` declares how Portico injects credentials into the downstream connection without exposing them to the calling agent:

| `auth.strategy` | What Portico does | Console `auth_state` badge |
|---|---|---|
| `env_inject` | Resolves <span v-pre>`{{secret:name}}`</span> placeholders in `stdio.env` and sets them in the subprocess environment. | `env` |
| `http_header_inject` | Resolves `auth.headers` values and sends them as request headers to an HTTP downstream. | `header` |
| `secret_reference` | Resolves a single `auth.secret_ref` vault key and injects it as a bearer token or into `http.auth_header`. | `vault_ref` |
| `oauth2_token_exchange` | Performs RFC 8693 token exchange on each session using the `auth.exchange` config. | `oauth` |

Credentials are resolved from the tenant-scoped vault at the time the supervisor starts (or restarts) a process. They are never forwarded to agents and never appear in audit logs.

See [Credentials vault](/concepts/credentials-vault) and [OAuth token exchange](/concepts/oauth-token-exchange) for vault setup and rotation procedures.

## Relation to other subsystems

- **Southbound transport** — the registry's `Snapshot` is consumed by the southbound manager to obtain or create a client connection. See [MCP southbound](/concepts/mcp-southbound).
- **Catalog and sessions** — tool/resource/prompt capabilities are indexed per session. The capability counts shown in the list response derive from the most recent catalog snapshot. See [Catalog and sessions](/concepts/catalog-and-sessions).
- **Policy** — the `auth.default_risk_class` on a server spec sets the floor for policy evaluation when neither a Skill Pack nor an explicit rule provides an override. See [Policy](/concepts/policy).
- **Console** — the `/servers` page in the operator Console renders live status, substrate fields, and the log stream. See [Console](/concepts/console).

## Related

- [MCP southbound](/concepts/mcp-southbound)
- [MCP northbound](/concepts/mcp-northbound)
- [Catalog and sessions](/concepts/catalog-and-sessions)
- [Credentials vault](/concepts/credentials-vault)
- [Policy](/concepts/policy)
- [Console](/concepts/console)
- [Configuration reference](/reference/configuration)
- [REST API reference](/reference/rest-api)
