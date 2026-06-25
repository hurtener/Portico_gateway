# MCP southbound (servers)

The southbound layer is Portico's client-side of MCP: it manages the fleet of downstream MCP
servers, one per registered entry in a tenant's [server registry](/concepts/mcp-registry). Every
tool call, resource read, and prompt invocation that an AI client triggers on the northbound side
ultimately routes through the southbound layer to the appropriate downstream process or HTTP
endpoint.

This page covers the runtime architecture: the `Client` interface, the two transports, the process
supervisor, runtime modes, idle and crash handling, the command allowlist, and how W3C trace context
propagates across process and network boundaries.

---

## The `Client` interface

All communication with a downstream MCP server goes through a single interface defined in
`internal/mcp/southbound/types.go`. Production code imports this interface only — never a concrete
transport directly.

```go
type Client interface {
    Start(ctx context.Context) error
    Initialized() bool
    Capabilities() protocol.ServerCapabilities
    ServerInfo()   protocol.Implementation
    Ping(ctx context.Context) error

    ListTools(ctx context.Context) ([]protocol.Tool, error)
    CallTool(ctx context.Context, name string, arguments json.RawMessage,
             progressToken json.RawMessage, progress ProgressCallback) (*protocol.CallToolResult, error)

    ListResources(ctx context.Context, cursor string) ([]protocol.Resource, string, error)
    ListResourceTemplates(ctx context.Context, cursor string) ([]protocol.ResourceTemplate, string, error)
    ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error)
    SubscribeResource(ctx context.Context, uri string) error
    UnsubscribeResource(ctx context.Context, uri string) error

    ListPrompts(ctx context.Context, cursor string) ([]protocol.Prompt, string, error)
    GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error)

    Notifications() <-chan protocol.Notification
    Close(ctx context.Context) error
}
```

`Start` performs the MCP `initialize` handshake and is idempotent — subsequent calls return the
cached result. `Notifications` returns a read-only channel of every non-progress notification the
downstream sends; the channel uses drop-oldest backpressure with a 32-slot buffer, so consumers
must drain promptly.

---

## Transports

Two concrete transports implement `Client`. Both live in subdirectories of
`internal/mcp/southbound/`; neither is imported by callers outside that subtree.

### stdio

The stdio transport (`internal/mcp/southbound/stdio/`) spawns a child process and speaks JSON-RPC
2.0 over its stdin and stdout. Each newline-delimited JSON object on stdout is either a response
(has `id`) or a notification (has `method`, no `id`). Stderr lines are forwarded to the gateway's
structured logger at `Debug` level and also written to a per-process log ring buffer, which the
`/api/servers/{id}/logs` SSE endpoint tails live.

Key implementation details:

- **Process group.** The child is started with `setpgid()` so that if the server forks helper
  processes, a SIGTERM or SIGKILL sent to the group terminates the whole tree.
- **Graceful shutdown.** `Close` sends SIGTERM, waits up to 3 seconds, then sends SIGKILL if the
  process has not exited.
- **Context cancellation.** When a caller's `ctx` is cancelled while a `call` is in flight, the
  client sends a `notifications/cancelled` message to the downstream and returns `ctx.Err()`.
- **Progress.** `CallTool` forwards the caller's `progressToken` to the downstream. Progress
  notifications matching that token are routed to the supplied `ProgressCallback` and do not appear
  on the `Notifications()` channel.
- **argv-form only.** The process is always started as `exec.Command(command, args...)` —
  never `sh -c "..."`. Shell expansion does not occur; the operator controls the exact argv.

### HTTP

The HTTP transport (`internal/mcp/southbound/http/`) targets the JSON-response variant of the MCP
Streamable HTTP specification: every request is a POST to the configured URL that returns
`application/json`. The client negotiates a session by echoing the `Mcp-Session-Id` header the
downstream assigns, and terminates the session with a DELETE on `Close`.

The `HeaderProvider` field in `httpclient.Config` is called on every outbound request. It receives
the per-request `context.Context` so credential resolution can be cancelled. Values from the
provider take precedence over the static `auth_header` config key.

::: info HTTP runtime mode
HTTP downstreams must use `runtime_mode: remote_static`. The supervisor registers them but does not
manage a process; lifecycle bookkeeping (idle, restart) does not apply to `remote_static` entries.
:::

---

## Manager

`internal/mcp/southbound/manager/Manager` is the seam the MCP gateway dispatcher uses. It translates
an `AcquireRequest` — which carries `TenantID`, `UserID`, `SessionID`, `ServerID`, plus any
credential-injector-resolved `AuthEnv` and `AuthHeaders` — into a started `Client`.

```go
client, err := manager.Acquire(ctx, manager.AcquireRequest{
    TenantID:  tenantID,
    ServerID:  serverID,
    SessionID: sessionID,
    AuthEnv:   resolvedEnv,     // stdlib KEY=VALUE pairs for stdio
    AuthHeaders: resolvedHdrs,  // per-request headers for HTTP
})
```

The Manager is a thin coordinator. The [server registry](/concepts/mcp-registry) holds the per-tenant
`ServerSpec`; the Supervisor owns the live process state. This separation avoids an import cycle
between the transport packages and the registry.

---

## Process supervisor

`internal/runtime/process/Supervisor` owns the full lifecycle of stdio and remote-static HTTP
instances. It stores one `instance` per `InstanceKey`; concurrent callers that race to acquire the
same key wait on a per-key channel rather than each spawning a duplicate.

### InstanceKey

The key uniquely identifies a supervised instance. Its shape depends on the runtime mode:

| Runtime mode     | Key fields                              | Process count                      |
|------------------|-----------------------------------------|------------------------------------|
| `shared_global`  | `ServerID`, `TenantID = "_global"`     | One per server across all tenants  |
| `per_tenant`     | `TenantID`, `ServerID`                 | One per tenant per server          |
| `per_user`       | `TenantID`, `UserID`, `ServerID`       | One per user per server            |
| `per_session`    | `TenantID`, `UserID`, `SessionID`, `ServerID` | One per MCP session         |
| `remote_static`  | `ServerID`, `TenantID = "_global"`     | No managed process (HTTP only)     |

`per_session` is the most isolated mode: each MCP session carries its own subprocess instance. Use
it when the downstream maintains user-scoped state or when credential isolation at the session
boundary is required.

### Instance lifecycle states

```
starting → running → idle      (idle timeout elapsed; process closed; restarts lazily)
running  → crashed             (health probes breached threshold)
crashed  → backoff             (restart attempted; failed)
backoff  → circuit_open        (max_restart_attempts exceeded)
circuit_open → starting        (circuit_open_duration elapsed; next Acquire retries)
running  → stopping            (explicit Stop or server removed from registry)
```

When the registry notifies the supervisor of a server update (via `OnUpdated`), all running
instances for that server are drained. The next `Acquire` picks up the new spec. When a server is
removed (`OnRemoved`), its instances are stopped immediately.

---

## Idle timeouts

When `lifecycle.idle_timeout` is set on a server spec, the supervisor arms an `IdleTimer` for each
instance. Every successful `Acquire` calls `tickIdle`, which resets the deadline. When the timer
fires — meaning no call arrived within the timeout — the supervisor closes the process and
transitions the instance to `idle`. The next `Acquire` relaunches it lazily.

```yaml
servers:
  - id: filesystem
    transport: stdio
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
    lifecycle:
      idle_timeout: 10m   # close process after 10 minutes of inactivity
```

Set `idle_timeout: 0` (the default) to disable idle eviction entirely.

---

## Health probes and crash recovery

When `health.ping_interval` is set, the supervisor starts a background goroutine per instance that
calls `Client.Ping` on the configured cadence. Three consecutive failures transition the instance to
`crashed` and close the client. The next `Acquire` triggers a restart.

Restarts use exponential backoff:

| Parameter              | Default   | Config key                      |
|------------------------|-----------|---------------------------------|
| Initial delay          | 500 ms    | `lifecycle.backoff_initial`     |
| Maximum delay          | 30 s      | `lifecycle.backoff_max`         |
| Multiplier             | 2.0       | —                               |
| Jitter                 | ±20%      | —                               |
| Max restart attempts   | 5         | `lifecycle.max_restart_attempts`|
| Circuit-open duration  | 5 minutes | `lifecycle.circuit_open_duration`|

When `max_restart_attempts` consecutive restarts all fail, the instance moves to `circuit_open`.
Subsequent `Acquire` calls receive an immediate error that includes the time until the circuit
resets. After `circuit_open_duration` elapses, the next `Acquire` allows a fresh start attempt.

```yaml
servers:
  - id: github
    transport: stdio
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
    health:
      ping_interval: 30s
      ping_timeout: 5s
    lifecycle:
      max_restart_attempts: 3
      circuit_open_duration: 2m
```

---

## Command allowlist for stdio

Stdio servers run operator-supplied commands from configuration. The `auth.command_allowlist` field
on a server spec restricts which executable names the supervisor is permitted to spawn. When the
allowlist is non-empty, a spawn whose `stdio.command` does not appear in the list is rejected before
the process starts.

```yaml
servers:
  - id: code-assistant
    transport: stdio
    stdio:
      command: node
      args: ["/opt/mcp/server.js"]
    auth:
      command_allowlist: [node, python3, npx]
```

The check applies at spawn time, including lazy restarts. The supervisor always invokes the process
in argv-form (`exec.Command(command, args...)`), never via a shell string, so the allowlist checks
against the actual executable name without shell expansion hazards.

---

## Credential injection

The supervisor merges credentials from the [credentials vault](/concepts/credentials-vault) into
the child environment before spawning. The `auth.env` field (for `env_inject` strategy) accepts
<span v-pre>`KEY={{secret:name}}`</span> and <span v-pre>`KEY={{env:NAME}}`</span> placeholders. The Resolver expands these at spawn
time using the vault keyed on `(tenantID, secretName)`:

```yaml
auth:
  strategy: env_inject
  env:
    - GITHUB_TOKEN={{secret:gh_pat}}
    - AWS_REGION={{env:AWS_REGION}}
```

For HTTP downstreams, `auth.headers` carries the equivalent: header name → value with optional
<span v-pre>`{{secret:name}}`</span> placeholders, resolved by the `HeaderProvider` on every outbound request.

See [Credentials vault](/concepts/credentials-vault) and [OAuth token exchange](/concepts/oauth-token-exchange)
for the full credential strategy reference.

---

## Trace context propagation

Portico propagates W3C Trace Context across every southbound boundary so downstream MCP servers can
link their spans into the same distributed trace as the gateway.

### HTTP downstream

On every outbound HTTP request, `telemetry.InjectIntoHTTP` writes the active trace context into the
request headers using the OTel propagator registered in the gateway. A downstream server that
instruments itself with OpenTelemetry (or any W3C-compliant tracer) will automatically attach its
spans to the parent trace.

```
POST /mcp HTTP/1.1
traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
tracestate:  ...
```

### stdio downstream

Stdio servers receive the active trace context via environment variables injected at spawn time:

```
MCP_TRACEPARENT=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
TRACEPARENT=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
```

Both variables carry the same value. `MCP_TRACEPARENT` is the MCP-conventional key;
`TRACEPARENT` is provided for servers that check for the W3C key directly. Injection is
best-effort: if no span is active in the gateway's context at spawn time, neither variable is set
and the child starts without a parent span.

On the inbound (northbound) side, Portico extracts trace context from both the `traceparent` HTTP
header and from `_meta.traceparent` in the MCP request body, so stdio-originating northbound
clients can also participate in the trace. See [Observability](/concepts/observability) for the
full telemetry configuration.

---

## Server spec reference

The fields below map directly onto `registry.ServerSpec`. Durations accept human strings (`"5s"`,
`"1m30s"`).

```yaml
servers:
  - id: my-server                    # required; lowercase alphanumeric, _ or -
    display_name: My Server          # optional; defaults to id
    transport: stdio                 # stdio | http
    runtime_mode: per_tenant         # shared_global | per_tenant | per_user | per_session | remote_static

    # --- stdio only ---
    stdio:
      command: npx                   # required; executable name (argv-form)
      args: ["-y", "@acme/mcp"]      # optional; additional argv
      env: ["PORT=8080"]             # optional; static env vars
      cwd: /opt/server               # optional; working directory
      start_timeout: 10s             # default 10s; initialize handshake deadline

    # --- http only (runtime_mode must be remote_static) ---
    http:
      url: https://mcp.example.com/mcp  # required
      auth_header: "Bearer static-tok"  # optional; static Authorization header
      timeout: 30s                       # default 30s; per-request timeout

    # --- health probes ---
    health:
      ping_interval: 30s             # 0 = disabled
      ping_timeout: 5s               # default 5s
      startup_grace: 5s              # default 5s; suppress probes after spawn

    # --- lifecycle ---
    lifecycle:
      idle_timeout: 10m              # 0 = disabled
      backoff_initial: 500ms         # default 500ms
      backoff_max: 30s               # default 30s
      max_restart_attempts: 5        # default 5; triggers circuit breaker
      circuit_open_duration: 5m      # default 5m
      shutdown_grace: 5s             # default 5s

    # --- credentials ---
    auth:
      strategy: env_inject           # env_inject | http_header_inject | secret_reference |
                                     # oauth2_token_exchange | credential_shim
      env:
        - API_KEY={{secret:my_api_key}}
      command_allowlist: [npx]       # optional; restrict spawnable executables

    enabled: true                    # default true; false disables Acquire
```

---

## Related

- [MCP registry](/concepts/mcp-registry) — how server specs are stored, versioned, and scoped per tenant
- [Multi-tenancy](/concepts/multi-tenancy) — how `TenantID` flows into InstanceKey and isolates processes
- [Credentials vault](/concepts/credentials-vault) — managing secrets referenced in `auth.env` and `auth.headers`
- [OAuth token exchange](/concepts/oauth-token-exchange) — the `oauth2_token_exchange` auth strategy
- [MCP northbound](/concepts/mcp-northbound) — the inbound half: northbound sessions, SSE transport, capability negotiation
- [Observability](/concepts/observability) — distributed tracing, structured logs, and metrics
- [Register an MCP server](/guides/register-mcp-server) — step-by-step guide to adding a server to the registry
