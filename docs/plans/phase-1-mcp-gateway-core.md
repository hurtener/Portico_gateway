# Phase 1 — MCP Gateway Core

> Self-contained implementation plan. Builds on Phase 0 deliverables.

## Goal

Implement the MCP protocol layer: northbound (Portico-as-MCP-server to AI clients) and southbound (Portico-as-MCP-client to downstream MCP servers). After Phase 1, Portico can:
- Expose a single MCP HTTP+SSE endpoint to clients.
- Speak MCP to one or more downstream stdio and HTTP servers (registered statically in config — dynamic registry is Phase 2).
- Aggregate `tools/list` from downstream servers with namespacing.
- Route `tools/call` to the correct downstream server.
- Negotiate capabilities, propagate cancellation and progress, map errors.

Resources, prompts, and skills land in Phase 3 and Phase 4. Phase 1 stays narrowly focused on tools so the foundation can be tested against real downstream servers.

## Why this phase exists

Everything Portico does flows through the MCP protocol layer. Getting capability negotiation, namespacing, and error mapping right early makes every later phase cleaner. Aggregating tools across multiple downstream servers is the simplest non-trivial demonstration that the gateway works end-to-end.

## Prerequisites

Phase 0 is complete and merged. In particular:
- `cmd/portico` binary builds and runs.
- HTTP server with tenant middleware works.
- SQLite store with `servers` table exists (empty).
- Config loader parses `servers:` block (currently ignored by Phase 0; Phase 1 wires it).

## Deliverables

1. MCP protocol types in `internal/mcp/protocol/` (types only, no I/O).
2. Northbound MCP transport at `internal/mcp/northbound/http/` — Streamable HTTP per spec (POST + SSE).
3. Southbound stdio MCP client at `internal/mcp/southbound/stdio/`.
4. Southbound HTTP MCP client at `internal/mcp/southbound/http/`.
5. Server connection manager at `internal/mcp/southbound/manager.go` — holds open client connections to downstream servers.
6. Tool aggregator + namespacing at `internal/catalog/namespace/`.
7. Northbound route dispatcher at `internal/server/mcpgw/`.
8. Static server registration via config (Phase 2 makes it dynamic).
9. Tests covering: initialize handshake, tools/list aggregation, tools/call routing, cancellation, error mapping, transport edge cases.
10. Two mock MCP servers in `examples/servers/mock/` for integration tests.

## Acceptance criteria

1. With two mock downstream servers configured in `portico.yaml` (one stdio, one HTTP), an MCP client connecting to Portico's `/mcp` endpoint receives:
   - A successful `initialize` response advertising aggregated capabilities.
   - A `tools/list` response containing all tools from both downstream servers, namespaced as `{server_id}.{tool_name}`.
   - A successful `tools/call` for any namespaced tool, routed to the correct server.
2. Capability negotiation propagates: the gateway advertises only capabilities supported by at least one downstream (or the gateway itself).
3. Cancellation: an MCP client sending `notifications/cancelled` for a request causes the southbound call to be cancelled; the downstream server receives the cancellation; the response to the client is a `cancelled` error per spec.
4. Progress: progress notifications from downstream are forwarded to the northbound client, with the request ID re-mapped.
5. Error mapping: a downstream protocol error (`-32601 method not found`) reaches the northbound client unchanged in code; a downstream transport error becomes a Portico-defined `-32002 upstream_unavailable` with structured data.
6. Two simultaneous clients can talk to Portico independently; their sessions do not bleed into each other (verified by integration test).
7. Stdio downstream lifecycle is correct: process started lazily on first call; held open across calls; killed on Portico shutdown; restarted on crash with backoff.
8. Tenant scoping: tools list is filtered to tools whose server is enabled for the requesting tenant (Phase 2 implements per-tenant servers; Phase 1 uses a simple "all servers visible to all tenants" stub with a TODO).
9. `go test ./internal/mcp/... -race` passes.
10. End-to-end test: a real `@modelcontextprotocol/server-everything` (or equivalent) configured as stdio downstream, an `mcp` CLI client connects through Portico, lists and calls tools.

## Architecture

```
+---------------------------------------------------+
| Northbound HTTP+SSE                               |
| /mcp  (POST for requests, GET for SSE stream)     |
+---------------------+-----------------------------+
                      |
                      v
+---------------------------------------------------+
| internal/server/mcpgw                              |
|   Session registry                                 |
|   Request dispatcher                               |
|   Notification fanout                              |
+---------------------+-----------------------------+
                      |
                      v
+---------------------------------------------------+
| internal/mcp/northbound/http                       |
|   Streamable HTTP transport (frame parser)         |
+---------------------+-----------------------------+
                      |
            +---------+---------+
            v                   v
+-----------------------+ +-----------------------+
| Tool aggregator       | | Capability negotiator |
| (catalog/namespace)   | | (mcp/protocol/caps)   |
+-----------+-----------+ +-----------------------+
            |
            v
+---------------------------------------------------+
| internal/mcp/southbound/manager                    |
|   Map of (tenant, server_id) -> client connection  |
+----+----------------------+-----------------------+
     |                      |
     v                      v
+---------------+    +-----------------+
| stdio client  |    | HTTP+SSE client |
+---------------+    +-----------------+
     |                      |
     v                      v
  Downstream MCP server (stdio process)  /  Downstream MCP server (HTTP)
```

## Package layout (added in this phase)

```
internal/mcp/
  protocol/
    types.go              # JSON-RPC + MCP message structs
    methods.go            # method name constants
    errors.go             # error codes + helpers
    capabilities.go       # capability negotiation logic
    types_test.go
  northbound/
    http/
      transport.go        # Streamable HTTP server-side
      sse.go              # SSE writer
      session.go          # per-connection session state
      transport_test.go
  southbound/
    types.go              # Client interface
    manager.go            # connection pool, lifecycle
    stdio/
      client.go
      framing.go          # JSON-RPC line framing
      client_test.go
    http/
      client.go
      sse_reader.go
      client_test.go
    manager_test.go
internal/server/mcpgw/
  router.go               # /mcp routes
  dispatcher.go           # request → southbound call
  session_registry.go
  dispatcher_test.go
internal/catalog/
  namespace/
    namespace.go          # split/join, validation
    namespace_test.go
examples/servers/mock/
  mock.go                 # in-process mock MCP server (used in tests)
  cmd/mockmcp/main.go     # runnable for integration tests
test/integration/
  mcp_e2e_test.go
```

## MCP protocol types (Phase 1 subset)

We define our own protocol types rather than depending on an external SDK. This keeps Portico in full control of the wire format and makes future spec updates easy to handle. If `github.com/modelcontextprotocol/go-sdk` proves stable and complete during Phase 1 implementation, swap it in behind the same internal interfaces; the rest of Portico does not import the SDK directly.

```go
// internal/mcp/protocol/types.go
package protocol

import "encoding/json"

const ProtocolVersion = "2025-06-18" // pin the spec version Portico targets; bump explicitly

// JSON-RPC envelope
type Request struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *Error          `json:"error,omitempty"`
}

type Notification struct {
    JSONRPC string          `json:"jsonrpc"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type Error struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}

// Initialize
type InitializeParams struct {
    ProtocolVersion string             `json:"protocolVersion"`
    Capabilities    ClientCapabilities `json:"capabilities"`
    ClientInfo      Implementation     `json:"clientInfo"`
}

type InitializeResult struct {
    ProtocolVersion string             `json:"protocolVersion"`
    Capabilities    ServerCapabilities `json:"capabilities"`
    ServerInfo      Implementation     `json:"serverInfo"`
    Instructions    string             `json:"instructions,omitempty"`
}

type Implementation struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

// Capabilities
type ClientCapabilities struct {
    Roots       *RootsCapability    `json:"roots,omitempty"`
    Sampling    *SamplingCapability `json:"sampling,omitempty"`
    Elicitation *ElicitCapability   `json:"elicitation,omitempty"`
    Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

type ServerCapabilities struct {
    Tools     *ToolsCapability     `json:"tools,omitempty"`
    Resources *ResourcesCapability `json:"resources,omitempty"`
    Prompts   *PromptsCapability   `json:"prompts,omitempty"`
    Logging   *LoggingCapability   `json:"logging,omitempty"`
    Experimental map[string]json.RawMessage `json:"experimental,omitempty"`
}

type ToolsCapability struct {
    ListChanged bool `json:"listChanged,omitempty"`
}
type ResourcesCapability struct {
    Subscribe   bool `json:"subscribe,omitempty"`
    ListChanged bool `json:"listChanged,omitempty"`
}
type PromptsCapability struct {
    ListChanged bool `json:"listChanged,omitempty"`
}
type LoggingCapability struct{}
type RootsCapability struct {
    ListChanged bool `json:"listChanged,omitempty"`
}
type SamplingCapability struct{}
type ElicitCapability struct{}

// Tools
type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    InputSchema json.RawMessage `json:"inputSchema"`
    Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

type ToolAnnotations struct {
    Title          string `json:"title,omitempty"`
    ReadOnlyHint   *bool  `json:"readOnlyHint,omitempty"`
    DestructiveHint *bool `json:"destructiveHint,omitempty"`
    IdempotentHint *bool  `json:"idempotentHint,omitempty"`
    OpenWorldHint  *bool  `json:"openWorldHint,omitempty"`
}

type ListToolsResult struct {
    Tools      []Tool `json:"tools"`
    NextCursor string `json:"nextCursor,omitempty"`
}

type CallToolParams struct {
    Name      string          `json:"name"`
    Arguments json.RawMessage `json:"arguments,omitempty"`
}

type CallToolResult struct {
    Content []ContentBlock `json:"content"`
    IsError bool           `json:"isError,omitempty"`
    StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
}

type ContentBlock struct {
    Type     string          `json:"type"` // text | image | audio | resource
    Text     string          `json:"text,omitempty"`
    Data     string          `json:"data,omitempty"`     // base64
    MimeType string          `json:"mimeType,omitempty"`
    Resource *ResourceRef    `json:"resource,omitempty"`
}

type ResourceRef struct {
    URI      string `json:"uri"`
    MimeType string `json:"mimeType,omitempty"`
    Text     string `json:"text,omitempty"`
}

// Cancellation / progress
type CancelledParams struct {
    RequestID json.RawMessage `json:"requestId"`
    Reason    string          `json:"reason,omitempty"`
}
type ProgressParams struct {
    ProgressToken json.RawMessage `json:"progressToken"`
    Progress      float64         `json:"progress"`
    Total         *float64        `json:"total,omitempty"`
}
```

```go
// internal/mcp/protocol/methods.go
package protocol

const (
    MethodInitialize = "initialize"
    MethodInitialized = "notifications/initialized"
    MethodPing = "ping"
    MethodToolsList = "tools/list"
    MethodToolsCall = "tools/call"
    MethodResourcesList = "resources/list"
    MethodResourcesRead = "resources/read"
    MethodResourcesTemplatesList = "resources/templates/list"
    MethodPromptsList = "prompts/list"
    MethodPromptsGet = "prompts/get"

    NotifCancelled = "notifications/cancelled"
    NotifProgress = "notifications/progress"
    NotifToolsListChanged = "notifications/tools/list_changed"
    NotifResourcesListChanged = "notifications/resources/list_changed"
    NotifResourcesUpdated = "notifications/resources/updated"
    NotifPromptsListChanged = "notifications/prompts/list_changed"
)
```

```go
// internal/mcp/protocol/errors.go
package protocol

const (
    // JSON-RPC standard
    ErrParseError      = -32700
    ErrInvalidRequest  = -32600
    ErrMethodNotFound  = -32601
    ErrInvalidParams   = -32602
    ErrInternalError   = -32603

    // Portico-defined (per spec, -32000 to -32099 reserved for impl-specific)
    ErrApprovalRequired   = -32001
    ErrUpstreamUnavailable = -32002
    ErrPolicyDenied       = -32003
    ErrToolNotEnabled     = -32004
    ErrTenantInactive     = -32005
)

func NewError(code int, msg string, data any) *Error
```

## Capability negotiation

```go
// internal/mcp/protocol/capabilities.go
package protocol

// Aggregate computes the gateway's effective server capabilities given
// the union of downstream servers' caps. Conservative: a feature is on
// only if at least one downstream supports it.
func AggregateServerCaps(downstream []ServerCapabilities) ServerCapabilities

// NegotiateClientCaps takes the client caps from initialize and stashes
// them so dispatcher can decide whether to use elicitation vs fallback later.
type ClientCapsRecord struct {
    HasElicitation bool
    HasSampling    bool
    HasRoots       bool
}
func RecordClientCaps(c ClientCapabilities) ClientCapsRecord
```

Phase 1 advertises:
- `tools.listChanged: true` (gateway will emit on registry change)
- `resources` and `prompts` left nil — Phase 3 turns them on.
- `logging: {}` — basic logging supported.

## Northbound transport (Streamable HTTP)

The gateway exposes one endpoint, `POST /mcp`, that accepts a JSON-RPC request and returns either a JSON response or an SSE stream per spec.

### Wire shape

- **POST /mcp** with `Content-Type: application/json`:
  - If the request expects a single response (e.g. `tools/list` without progress), respond `200 OK` with `Content-Type: application/json` and the response body.
  - If the request may produce intermediate notifications (progress) or the client requested SSE, respond `200 OK` with `Content-Type: text/event-stream`, write each notification as `data: <json>\n\n`, and finally write the response and close the stream.
- **GET /mcp** with `Accept: text/event-stream`:
  - Opens a long-lived SSE channel for server-initiated notifications (e.g. `notifications/tools/list_changed`).
  - Session is identified by `Mcp-Session-Id` request header; if missing on first POST, gateway generates one and returns it as a response header `Mcp-Session-Id`. Subsequent requests must include it.
- **DELETE /mcp** with `Mcp-Session-Id`: explicit session termination.

### Session registry

```go
// internal/server/mcpgw/session_registry.go
package mcpgw

type Session struct {
    ID         string
    TenantID   string
    UserID     string
    ClientCaps protocol.ClientCapsRecord
    InitParams protocol.InitializeParams
    Notif      chan protocol.Notification // outbound notifications
    Created    time.Time
    LastSeen   time.Time
    cancel     context.CancelFunc
}

type SessionRegistry struct { /* sync.Map of ID -> *Session */ }

func (r *SessionRegistry) Create(tenantID, userID string) *Session
func (r *SessionRegistry) Get(id string) (*Session, bool)
func (r *SessionRegistry) Close(id string)
func (r *SessionRegistry) Touch(id string)
```

Sessions get persisted to the `sessions` table on creation and updated on closure.

### Transport implementation

```go
// internal/mcp/northbound/http/transport.go
package httpnb

type Handler struct {
    sessions *mcpgw.SessionRegistry
    dispatch Dispatcher
    log      *slog.Logger
}

type Dispatcher interface {
    HandleRequest(ctx context.Context, sess *mcpgw.Session, req *protocol.Request) (*protocol.Response, error)
    HandleNotification(ctx context.Context, sess *mcpgw.Session, notif *protocol.Notification) error
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) // dispatches POST/GET/DELETE
```

Streaming behavior:
- For requests that may produce progress notifications, the dispatcher returns a channel of notifications + a final response. The transport writes each notification as SSE then the final response, then closes.
- For the long-lived GET stream, the transport reads from `Session.Notif` and writes SSE frames; on session close, terminates the stream.

### Frame buffering

Per MCP spec, large messages are not chunked — the JSON object is the unit. SSE `data:` lines must not contain raw newlines; the transport must escape any embedded newlines before writing or use `event:` framing.

## Southbound clients

### Client interface

```go
// internal/mcp/southbound/types.go
package southbound

type Client interface {
    Initialize(ctx context.Context, params protocol.InitializeParams) (*protocol.InitializeResult, error)
    Ping(ctx context.Context) error

    ListTools(ctx context.Context) ([]protocol.Tool, error)
    CallTool(ctx context.Context, name string, args json.RawMessage, progress func(protocol.ProgressParams)) (*protocol.CallToolResult, error)

    // Phase 3 adds: ListResources, ReadResource, ListPrompts, GetPrompt.

    Capabilities() protocol.ServerCapabilities
    ServerInfo() protocol.Implementation

    Close(ctx context.Context) error

    // Notifications channel for upstream server-initiated notifications.
    Notifications() <-chan protocol.Notification
}
```

### Stdio client

```go
// internal/mcp/southbound/stdio/client.go
package stdiocli

type Config struct {
    Command string
    Args    []string
    Env     []string
    Cwd     string
    StartTimeout time.Duration  // default 10s
    Logger  *slog.Logger
}

type Client struct { /* stdin/stdout/stderr pipes, request map, mu, state */ }

func New(cfg Config) (*Client, error)            // does NOT start the process
func (c *Client) Start(ctx context.Context) error // start + initialize handshake
```

Internals:
- Spawn process with `os/exec`. Pipe stdin/stdout. Capture stderr into `Logger` with `slog.Warn` lines (treat as error stream).
- Reader goroutine reads `stdout` line-by-line (LSP-style framing: lines are JSON objects separated by `\n`, no Content-Length headers per MCP stdio spec).
- Maintain a map `requestID -> chan *Response`. Outgoing requests get a unique numeric ID.
- For incoming notifications, push into `Notifications()` channel (buffered, drop oldest with warning if backpressured).
- On `Close`, write a graceful close, then `cancel` reader, then `cmd.Process.Signal(os.Interrupt)`, then `Kill` after 5s if still alive.
- On unexpected EOF/process death, mark all pending requests as failed with `ErrUpstreamUnavailable` and close the notifications channel.

### HTTP client

```go
// internal/mcp/southbound/http/client.go
package httpcli

type Config struct {
    URL      string  // e.g. https://...mcp/v1
    AuthHeader string  // e.g. "Bearer XYZ" (post-Phase 5; Phase 1 leaves empty)
    Timeout  time.Duration
    Logger   *slog.Logger
}

type Client struct { /* http.Client + SSE reader */ }
```

Wire behavior mirrors the northbound transport (Streamable HTTP). Notifications come in via a long-lived SSE GET. Outgoing POST requests carry `Mcp-Session-Id`.

### Manager

```go
// internal/mcp/southbound/manager.go
package southbound

type Manager struct {
    log    *slog.Logger
    config []ServerSpec
    mu     sync.RWMutex
    conns  map[connKey]Client
}

type ServerSpec struct {
    ID          string
    Transport   string // stdio | http
    Stdio       *StdioSpec
    HTTP        *HTTPSpec
    RuntimeMode string  // shared_global | per_tenant | per_user | per_session | remote_static
    // Tenant scoping handled in Phase 2; Phase 1: all servers visible to all tenants.
}

type connKey struct {
    ServerID string
    TenantID string  // Phase 1: always "_global"
    UserID   string  // Phase 1: always ""
    SessionID string // Phase 1: only set for per_session servers
}

func (m *Manager) GetOrStart(ctx context.Context, key connKey) (Client, error)
func (m *Manager) Stop(ctx context.Context, key connKey) error
func (m *Manager) StopAll(ctx context.Context) error
func (m *Manager) AllForTenant(tenantID string) []connKey
```

In Phase 1, the manager honors only `shared_global` and `remote_static` (HTTP) modes — every other mode is treated as `shared_global`. Phase 2 extends.

## Tool aggregation + namespacing

### Namespacing

```go
// internal/catalog/namespace/namespace.go
package namespace

// Join: ("github", "get_pull_request") -> "github.get_pull_request"
// Split: "github.get_pull_request" -> ("github", "get_pull_request"), true
// Tools whose original name already contains a dot get the server prefix
// prepended; the original name is preserved (no parsing back-compat issues).

func Join(serverID, toolName string) string
func Split(qualified string) (serverID, toolName string, ok bool)

// Validate: enforce server IDs match ^[a-z0-9][a-z0-9_-]{0,31}$
func ValidateServerID(id string) error
```

If two downstream servers expose tools with the same name, namespacing prevents collision. The original tool name is preserved on the wire as `name` in `CallToolParams.Name` (Portico strips the prefix before sending to the downstream).

### Aggregator

```go
// internal/server/mcpgw/dispatcher.go
package mcpgw

type Dispatcher struct {
    sessions *SessionRegistry
    manager  southbound.Manager
    log      *slog.Logger
    metrics  *Metrics // Phase 6 wires real metrics
}

func (d *Dispatcher) HandleRequest(ctx context.Context, sess *Session, req *protocol.Request) (*protocol.Response, error)
```

Handling table:

| Method        | Behavior                                                              |
|---------------|-----------------------------------------------------------------------|
| `initialize`  | Validate version. Record client caps. Aggregate downstream caps. Return result. |
| `ping`        | Respond `{}`.                                                         |
| `tools/list`  | For each downstream server enabled for tenant, fan out `ListTools` (concurrent, with timeout). Aggregate, prefix names, return. Cache for the session for 60s. |
| `tools/call`  | Split name → (server, tool). Look up southbound client. `CallTool` with progress callback that fans notifications back into `sess.Notif`. Map errors. |
| Anything else | Return `ErrMethodNotFound` (Phase 3+ extends).                         |

`tools/list` aggregation must be tolerant: if one downstream errors or times out, log a warning, emit an audit event, and still return the others' tools. Set a global timeout (5s default) for the aggregation.

## Error mapping

Downstream errors flow up:
- Standard JSON-RPC codes (`-32601`, `-32602`, `-32603`) preserved with their original `data`.
- Transport-layer failures (process dead, HTTP unreachable, timeout) → `ErrUpstreamUnavailable` with `data: {"server_id": "...", "reason": "..."}`.
- Cancellation → JSON-RPC `-32800` cancelled (per spec) — _note: the spec uses a notification + the original request gets no response. Portico mirrors this faithfully._

## Configuration extensions

`portico.yaml` `servers:` block (parsed-but-ignored in Phase 0; wired here):

```yaml
servers:
  - id: github
    transport: stdio
    runtime_mode: shared_global    # Phase 1 supports shared_global + remote_static
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        - "GITHUB_TOKEN={{secret:github_token}}"   # placeholder; Phase 5 implements interpolation
      cwd: ""
      start_timeout: 10s
  - id: weather
    transport: http
    runtime_mode: remote_static
    http:
      url: https://weather.example.com/mcp
      auth_header: ""              # Phase 5
      timeout: 30s
```

## Mock MCP servers (for testing)

Two flavors:
- **In-process mock** at `examples/servers/mock/mock.go` — used directly by Go tests via an in-memory transport pair.
- **Standalone mock binary** at `examples/servers/mock/cmd/mockmcp/main.go` — speaks stdio for integration tests.

Mock features:
- Configurable list of tools (via env or flags).
- Configurable list-changed cadence.
- A `slow_tool` that takes a configurable duration and emits progress notifications.
- An `error_tool` that returns `-32603 internal_error` with structured data.
- A `cancel_test` tool that respects cancellation.

```go
// examples/servers/mock/mock.go
package mock

type Server struct {
    Name    string
    Version string
    Tools   []protocol.Tool
    Handler func(ctx context.Context, name string, args json.RawMessage, progress func(protocol.ProgressParams)) (*protocol.CallToolResult, error)
}

func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error
```

## Test plan

### Unit

- `internal/mcp/protocol/types_test.go`
  - `TestRequestRoundTrip` — marshal/unmarshal a Request with various ID types (string, int, null).
  - `TestErrorRoundTrip`, `TestNotificationRoundTrip`.
  - `TestAggregateServerCaps` — combinations of {tools-only}, {tools+resources}, etc.

- `internal/catalog/namespace/namespace_test.go`
  - `TestJoinSplit_Roundtrip` — join then split returns original parts.
  - `TestSplit_NoSeparator_ReturnsFalse`.
  - `TestSplit_DotInToolName` — `github.foo.bar` → ("github", "foo.bar").
  - `TestValidateServerID_*` — valid/invalid cases.

- `internal/mcp/southbound/stdio/client_test.go`
  - `TestStdioClient_InitializeAndListTools` — start mock binary, init, list, expect 3 tools.
  - `TestStdioClient_CallTool` — call `echo` tool, expect echoed content.
  - `TestStdioClient_CallToolProgress` — call `slow_tool`, capture progress callbacks, expect ≥2 progress events before result.
  - `TestStdioClient_Cancellation` — start a long call, cancel ctx, expect cancelled error within 200ms.
  - `TestStdioClient_ProcessCrash` — kill mock mid-call, expect ErrUpstreamUnavailable + Notifications channel closed.

- `internal/mcp/southbound/http/client_test.go`
  - `TestHTTPClient_InitializeAndListTools` — against a `httptest.Server` running mock.
  - `TestHTTPClient_StreamingResponse` — verify SSE-encoded progress events parse correctly.
  - `TestHTTPClient_RetryOnTransportError` — 503 once then 200; expect single retry.

- `internal/mcp/northbound/http/transport_test.go`
  - `TestPostInitialize_NewSession` — POST initialize, expect Mcp-Session-Id response header.
  - `TestPostListTools_AggregatesAcrossServers` — two mock downstreams, expect combined list with prefixes.
  - `TestPostCallTool_RoutingByPrefix` — call `github.foo`, expect routed to github mock; call `weather.bar` to weather mock.
  - `TestPostCallTool_UnknownPrefix` — expect ErrToolNotEnabled with structured data.
  - `TestGetSSE_NotificationFanout` — open SSE, trigger a downstream list_changed, expect northbound notification.
  - `TestDelete_TerminatesSession` — DELETE, then subsequent POST with same session id returns 404.

- `internal/server/mcpgw/dispatcher_test.go`
  - `TestDispatcher_ToolListAggregationTolerantToOneDownstreamError` — one downstream errors, the other returns tools; expect non-error response with the second's tools and an audit warning.
  - `TestDispatcher_TooManyConcurrentSessions` — load 100 sessions; expect no goroutine leak after 100 closes.

### Integration

- `test/integration/mcp_e2e_test.go`
  - `TestE2E_StdioDownstream` — spawn `mockmcp` binary as stdio downstream; spin up gateway; use Go HTTP client to drive Streamable HTTP; verify init → list → call.
  - `TestE2E_HTTPDownstream` — spin up `mockmcp` in HTTP mode (httptest); same flow.
  - `TestE2E_TwoConcurrentClients` — two goroutines drive different sessions through the gateway, each calls a slow tool with progress; assert no cross-talk.
  - `TestE2E_DownstreamCrashRecovery` — kill the stdio downstream mid-test; next call returns ErrUpstreamUnavailable; subsequent calls succeed (manager restarts the process — restart handled in Phase 2; Phase 1 expectation is upstream_unavailable with no auto-restart).

### Fixtures

- `examples/servers/mock/testdata/tools.json` — declarative tool definitions used by both Go-import and standalone variants.

## Common pitfalls

- **Stdio framing**: MCP stdio uses one JSON object per line. Don't use the LSP-style `Content-Length` framing — that's a different protocol.
- **stderr from downstream**: stdout is the JSON-RPC channel; stderr is for human logs. Many real MCP servers print debug info to stderr — capture it and emit at `debug` or `info` level, not `error` (only treat exit-related messages as errors).
- **SSE flushing**: every SSE write needs `flusher.Flush()` after the `\n\n`; otherwise notifications buffer in the OS.
- **Session ID generation**: use a cryptographic RNG (e.g. `crypto/rand` + base64url, 16 bytes). The session ID is auth-adjacent: a leaked one lets an attacker hijack an active session.
- **Cancellation propagation**: when the northbound client sends `notifications/cancelled` for a request ID, the dispatcher must (a) cancel the southbound `context.Context` for that call and (b) forward `notifications/cancelled` to the downstream server with the *downstream's* request ID, which differs from the northbound ID.
- **Request ID mapping**: northbound and southbound use independent ID spaces. The dispatcher maintains `northID -> southID` per session per request.
- **Progress token**: per spec, progress is opt-in via `_meta.progressToken` in the original params. Don't emit progress unless the client included a token.
- **Capability echo**: the gateway must NOT advertise a capability the dispatcher does not implement. If `tools/list_changed` is advertised but never sent, clients can hang waiting for it.
- **Backpressure on Notifications channel**: bounded channel (e.g. 256). Drop oldest with a `notification_dropped` audit event rather than blocking the reader goroutine.

## Out of scope

- Resources, prompts, resource templates (Phase 3).
- MCP Apps `ui://` handling (Phase 3).
- Skill exposure as MCP resources/prompts (Phase 4).
- Process supervision policies — health checks, idle timeout, crash backoff (Phase 2). Phase 1 has minimal "start on first call, kill on shutdown".
- Full tenant-scoped server routing (Phase 2). Phase 1 stub: all tenants can see all servers.
- OAuth token exchange / credential injection (Phase 5).
- OpenTelemetry tracing on dispatch (Phase 6).
- Catalog snapshots (Phase 6) — Phase 1's `tools/list` cache is a 60s in-memory aside, not a real snapshot.

## Done definition

1. All acceptance criteria pass.
2. All listed tests pass with `-race`.
3. Coverage ≥ 75% for `internal/mcp/protocol`, `internal/catalog/namespace`, `internal/mcp/southbound`.
4. The Console `/servers` page lists configured servers (read from config) — no real lifecycle yet, but visible.
5. A demo command works:
   ```bash
   ./bin/portico dev --config examples/dev-mock.yaml &
   curl -s -X POST http://localhost:8080/mcp \
     -H 'Content-Type: application/json' \
     -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
   ```
   Returns a valid `initialize` result.

## Hand-off to Phase 2

Phase 2 inherits:
- A working MCP gateway with static server registration.
- Southbound clients for stdio + HTTP.
- Tool aggregation + dispatch.
- Manager with stub lifecycle.

Phase 2's first job: replace static config-driven server registration with a dynamic registry (CRUD via API + hot-reload from YAML), implement the four V1 runtime modes (`shared_global`, `per_tenant`, `per_user`, `per_session`), and add full process supervision (health checks, idle timeout, crash recovery with backoff, resource limits, env injection, log capture).
