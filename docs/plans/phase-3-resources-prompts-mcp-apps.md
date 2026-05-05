# Phase 3 — Resources, Prompts, MCP Apps

> Self-contained implementation plan. Builds on Phase 0–2.

## Goal

Extend Portico's MCP surface from "tools only" to the **full V1 MCP surface**: resources, resource templates, prompts, and MCP Apps (`ui://` resources). Add list-changed handling that defaults to stable session catalogs but allows opt-in live updates. Enforce CSP and resource sandboxing on `ui://` content. After Phase 3, an MCP client can read documentation resources, fetch prompts, and render UI panels through the gateway across multiple downstream servers — all aggregated, namespaced, and policy-aware.

## Why this phase exists

Tools alone do not deliver the MCP value proposition. Resources let agents ground reasoning on documentation; prompts give them re-usable scaffolds; MCP Apps make tool results visualizable in the host UI. Putting these in before Skills (Phase 4) means the Skill runtime can lean on the same plumbing instead of duplicating it.

## Prerequisites

Phases 0–2 complete. In particular:
- Dispatcher in `internal/server/mcpgw` handles requests for tools.
- Southbound `Client` interface exists with stubs for `ListResources`, `ReadResource`, `ListPrompts`, `GetPrompt` (Phase 1 left them unimplemented; this phase fills them in).
- Registry tracks servers with their advertised capabilities.

## Deliverables

1. Southbound client methods for resources and prompts (stdio + HTTP impls).
2. Dispatcher routes for `resources/list`, `resources/read`, `resources/templates/list`, `prompts/list`, `prompts/get`.
3. Aggregator handles namespacing for resources (URI rewrite) and prompts (name prefix).
4. List-changed handling: per-session policy `stable | live`; default `stable`. Live mode forwards `notifications/resources/list_changed` and `notifications/prompts/list_changed`.
5. MCP Apps support: `ui://` URI scheme detection, CSP injection on `text/html` content, sandbox metadata propagated to the client.
6. Resource size limits: configurable per-tenant max bytes; oversized reads return a Portico-defined truncation marker plus an `artifact://` reference (artifacting infra arrives here; Phase 5 logs it).
7. App registry: a separate index of every `ui://` resource discovered across servers, with metadata used by the Console and Skill Pack `ui` bindings.
8. Console pages: `/resources`, `/prompts`, `/apps` show live aggregates; per-server detail pages link in.
9. Tests: aggregation, URI rewriting (round-trip), MIME inference, list-changed suppression, CSP injection, MCP Apps indexing, large resource truncation.

## Acceptance criteria

1. With a downstream server `github` and a downstream server `postgres`, both exposing distinct resources, an MCP client gets a single `resources/list` response that includes resources from both, namespaced as:
   - `mcp+server://{server_id}/{original_uri}` for non-`ui://` resources.
   - `ui://{server_id}/{original_path}` for MCP App resources.
   The original URI is preserved in `_meta.upstreamURI`.
2. `resources/read` for a namespaced URI is correctly routed to the right downstream server with the original URI restored.
3. `resources/templates/list` aggregates similarly and parameter substitution flows through unchanged.
4. `prompts/list` aggregates with names prefixed `{server_id}.{prompt_name}`. `prompts/get` strips the prefix and routes.
5. List-changed: a downstream server emitting `notifications/resources/list_changed` while a session is in `stable` mode does not propagate to the client; an audit event is emitted; a refreshed snapshot is computed lazily on next list call. Same with `live` mode but the notification *is* forwarded immediately.
6. MCP Apps: a downstream resource at `ui://github/code-review-panel.html` with `mimeType: text/html` arrives at the client wrapped with a strong CSP header (`default-src 'self'`; configurable allowlist) and an `iframe-sandbox` attribute hint in `_meta.portico.sandbox`. The Console `/apps` page lists this resource and shows a preview iframe in dev mode.
7. Resource read with a 50 MB body and a `max_resource_bytes: 1MB` tenant limit returns a truncated 1 MB body plus a `_meta.portico.truncated: true`, `_meta.portico.artifact_uri: artifact://{id}` (Phase 5 stores it; Phase 3 logs).
8. Resource MIME types correctly inferred when downstream returns just bytes: extension-based fallback (`.md` → `text/markdown`, `.json` → `application/json`, `.html` → `text/html`).
9. `resources/templates/list` returns aggregated templates across servers, with template URIs namespaced; `resources/read` for a substituted URI works.
10. `go test ./...` passes; integration tests cover at least 3 downstream servers with overlapping resource names.

## Architecture additions

```
              +-------------------------------+
              | Dispatcher (Phase 1)           |
              |  + resources / prompts / apps  |
              +-------+--------+---------------+
                      |        |
                      v        v
            +-----------------------------+
            | Resource Aggregator          |
            |   URI namespace + restore    |
            |   MIME inference             |
            |   Size limits + artifacting  |
            +--------------+---------------+
                           |
                           v
            +-----------------------------+
            | App Registry                 |
            |   ui:// indexer + CSP        |
            |   Sandbox metadata           |
            +-----------------------------+

            +-----------------------------+
            | Prompt Aggregator            |
            |   Name namespace + restore   |
            |   Argument validation        |
            +-----------------------------+

            +-----------------------------+
            | List-Changed Mux             |
            |   per-session mode           |
            |   notification forwarding    |
            +-----------------------------+
```

## Package layout

```
internal/mcp/southbound/
  stdio/client.go        # extend with resource/prompt methods
  http/client.go         # extend
internal/server/mcpgw/
  dispatcher.go          # extend handler table
  resources.go           # resource aggregator
  prompts.go             # prompt aggregator
  listchanged.go         # mux
internal/apps/
  registry.go            # ui:// index
  csp.go                 # CSP composer
  registry_test.go
internal/catalog/namespace/
  uri.go                 # URI rewrite + restore
  uri_test.go
internal/audit/
  artifact_stub.go       # placeholder; real artifact store in post-V1
internal/server/api/
  handlers_resources.go
  handlers_prompts.go
  handlers_apps.go
web/console/src/routes/
  resources/+page.svelte
  prompts/+page.svelte
  apps/+page.svelte
test/integration/
  resources_e2e_test.go
  prompts_e2e_test.go
  apps_e2e_test.go
```

## Public types and interfaces

### Southbound client extensions

Already declared in Phase 1 `Client` interface. Implement now:

```go
type Client interface {
    // ... Phase 1 methods ...

    ListResources(ctx context.Context, cursor string) ([]protocol.Resource, string, error)
    ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error)
    ListResourceTemplates(ctx context.Context, cursor string) ([]protocol.ResourceTemplate, string, error)

    ListPrompts(ctx context.Context, cursor string) ([]protocol.Prompt, string, error)
    GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error)

    SubscribeResource(ctx context.Context, uri string) error      // gated by capability
    UnsubscribeResource(ctx context.Context, uri string) error
}
```

### Protocol additions

```go
// internal/mcp/protocol/resources.go
type Resource struct {
    URI         string          `json:"uri"`
    Name        string          `json:"name,omitempty"`
    Description string          `json:"description,omitempty"`
    MimeType    string          `json:"mimeType,omitempty"`
    Annotations *Annotations    `json:"annotations,omitempty"`
    Size        *int64          `json:"size,omitempty"`
    Meta        json.RawMessage `json:"_meta,omitempty"`
}

type ResourceTemplate struct {
    URITemplate string       `json:"uriTemplate"`
    Name        string       `json:"name,omitempty"`
    Description string       `json:"description,omitempty"`
    MimeType    string       `json:"mimeType,omitempty"`
    Annotations *Annotations `json:"annotations,omitempty"`
}

type ListResourcesResult struct {
    Resources  []Resource `json:"resources"`
    NextCursor string     `json:"nextCursor,omitempty"`
}

type ReadResourceResult struct {
    Contents []ResourceContent `json:"contents"`
}

type ResourceContent struct {
    URI      string          `json:"uri"`
    MimeType string          `json:"mimeType,omitempty"`
    Text     string          `json:"text,omitempty"`
    Blob     string          `json:"blob,omitempty"` // base64
    Meta     json.RawMessage `json:"_meta,omitempty"`
}

// internal/mcp/protocol/prompts.go
type Prompt struct {
    Name        string             `json:"name"`
    Description string             `json:"description,omitempty"`
    Arguments   []PromptArgument   `json:"arguments,omitempty"`
}

type PromptArgument struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    Required    bool   `json:"required,omitempty"`
}

type ListPromptsResult struct {
    Prompts    []Prompt `json:"prompts"`
    NextCursor string   `json:"nextCursor,omitempty"`
}

type GetPromptResult struct {
    Description string          `json:"description,omitempty"`
    Messages    []PromptMessage `json:"messages"`
}

type PromptMessage struct {
    Role    string         `json:"role"` // user|assistant|system
    Content ContentBlock   `json:"content"`
}
```

### URI namespace

```go
// internal/catalog/namespace/uri.go
package namespace

// Rewrite a downstream URI for client exposure.
// rules:
//   - "ui://..."           -> "ui://{server}/{rest}"  (preserve scheme; first path segment is server)
//   - "file://..."         -> "mcp+server://{server}/file/{path}"
//   - "https://..."        -> "mcp+server://{server}/https/{authority}/{path}"
//   - any other            -> "mcp+server://{server}/raw/{base64url(originalURI)}"
//
// All rewrites preserve a header _meta.upstreamURI = original.

func RewriteResourceURI(serverID, original string) (rewritten string, sandbox bool)

// Restore: parse a rewritten URI back to (server, originalURI).
// sandbox=true means the URI is ui:// (caller will fetch from MCP Apps registry).
func RestoreResourceURI(rewritten string) (serverID, original string, isUI bool, ok bool)

// Prompt name namespace: "{server}.{name}" join/split.
func RewritePromptName(serverID, original string) string
func RestorePromptName(rewritten string) (serverID, original string, ok bool)
```

URI scheme rationale: `mcp+server://` is a Portico-internal scheme, opaque to the client. Clients pass it back unchanged in `resources/read`. The double-encoding of arbitrary schemes (`raw/{base64url}`) keeps the rewrite reversible without parsing weird URIs.

### Resource aggregator

```go
// internal/server/mcpgw/resources.go
package mcpgw

type ResourceAggregator struct {
    log     *slog.Logger
    sup     *process.Supervisor
    registry *registry.Registry
    apps    *apps.Registry
    limits  ResourceLimits
}

type ResourceLimits struct {
    MaxBytesPerRead int64  // default 10 MB; per-tenant override
}

func (a *ResourceAggregator) ListAll(ctx context.Context, sess *Session, cursor string) (*protocol.ListResourcesResult, error)
func (a *ResourceAggregator) Read(ctx context.Context, sess *Session, uri string) (*protocol.ReadResourceResult, error)
func (a *ResourceAggregator) ListTemplates(ctx context.Context, sess *Session, cursor string) ([]protocol.ResourceTemplate, string, error)
```

`ListAll` flow:
1. For each server enabled for the session's tenant, call `client.ListResources(ctx, "")` concurrently (timeout 5s).
2. Tolerant: errors are logged + audited; partial results are returned.
3. For each resource:
   - Detect `ui://` scheme. If present, register with `apps.Registry` and rewrite URI per the rules above.
   - For non-UI: rewrite URI.
   - Add `_meta.upstreamURI = original`.
   - Set `_meta.serverID`.
4. Sort: alphabetical by rewritten URI (deterministic for snapshot hashes in Phase 6).
5. Cursor: aggregator does its own paging if `cursor != ""` — the gateway flattens per-server pagination into a global cursor, opaque to the client (encode as base64-JSON `{server: cursor, ...}`).

`Read` flow:
1. `RestoreResourceURI(uri)` → (server, original, isUI).
2. If `isUI`, check `apps.Registry` first for cached metadata; fetch via downstream regardless (content may be dynamic).
3. Look up southbound client via supervisor.
4. Call `client.ReadResource(ctx, original)`.
5. Apply size limit: if any single content exceeds `MaxBytesPerRead`, truncate, set `_meta.portico.truncated: true`, `_meta.portico.artifact_uri: artifact://{id}` (logged for now; persisted in Phase 5).
6. If `isUI` and any content has `mimeType: text/html`, run through `apps.csp.Compose` to wrap with CSP and sandbox hints.

### Prompt aggregator

```go
// internal/server/mcpgw/prompts.go
package mcpgw

type PromptAggregator struct {
    log     *slog.Logger
    sup     *process.Supervisor
    registry *registry.Registry
}

func (a *PromptAggregator) ListAll(ctx context.Context, sess *Session, cursor string) (*protocol.ListPromptsResult, error)
func (a *PromptAggregator) Get(ctx context.Context, sess *Session, name string, args map[string]string) (*protocol.GetPromptResult, error)
```

Aggregation: same fan-out + tolerant collection. Names are prefixed `{server}.{name}`. `Get` splits and routes.

### List-changed mux

```go
// internal/server/mcpgw/listchanged.go
package mcpgw

type ListChangedMux struct {
    sessions  *SessionRegistry
    log       *slog.Logger
    audit     audit.Emitter
}

type ListChangedMode string
const (
    ModeStable ListChangedMode = "stable"
    ModeLive   ListChangedMode = "live"
)

// SetMode is called from initialize params; clients opt into live via
// `experimental.portico.listChanged: "live"` in capabilities.
func (m *ListChangedMux) SetMode(sessionID string, mode ListChangedMode)

// OnDownstream is called by southbound notifications channel.
func (m *ListChangedMux) OnDownstream(serverID string, notif protocol.Notification)
```

Behavior:
- Stable mode: notification is dropped at the mux; an audit event `list_changed_suppressed` is emitted with details. The aggregator's cache is invalidated so the next list call returns fresh content. This realizes "stable catalogs by default" — the client sees changes only when it asks.
- Live mode: notification is forwarded to all sessions of the same tenant whose servers include the originating server. The notification is rewritten to use the namespaced URI scheme (e.g. resources notifications gain prefix info via params).

### Apps registry

```go
// internal/apps/registry.go
package apps

type Registry struct {
    mu     sync.RWMutex
    items  map[string]*App  // key = ui://{server}/{path}
    cspCfg CSPConfig
}

type App struct {
    URI         string         // canonical ui://server/path
    UpstreamURI string         // original
    ServerID    string
    Name        string
    Description string
    MimeType    string
    Annotations json.RawMessage
    DiscoveredAt time.Time
}

type CSPConfig struct {
    DefaultSrc []string  // default ["'self'"]
    ScriptSrc  []string
    StyleSrc   []string
    ImgSrc     []string
    ConnectSrc []string
    FrameSrc   []string
    Sandbox    string  // e.g. "allow-scripts allow-forms"; default "allow-scripts"
}

func New(cfg CSPConfig) *Registry
func (r *Registry) Register(a *App)
func (r *Registry) Lookup(uri string) (*App, bool)
func (r *Registry) ListByTenant(ctx, tenantID string) []*App  // intersected with registry/policy in Phase 5
```

### CSP composer

```go
// internal/apps/csp.go
package apps

func (c CSPConfig) Header() string  // assemble CSP header value

// Compose wraps an HTML body with CSP enforcement. Strategy:
//   - Inject a <meta http-equiv="Content-Security-Policy" content="..."> as the first child of <head>.
//   - If there's no <head>, create one.
//   - Add _meta.portico.sandbox = c.Sandbox to outer ResourceContent.
//   - Add _meta.portico.csp = c.Header() too, so hosts can also set the HTTP header
//     when serving via SSE (which doesn't carry HTTP response headers per resource).
func (c CSPConfig) Compose(html []byte) ([]byte, map[string]string, error)
```

CSP is conservative by default. Operators can loosen via config:

```yaml
apps:
  csp:
    default_src: ["'self'"]
    script_src: ["'self'", "'unsafe-inline'"]   # operators may need this for some MCP Apps
    style_src: ["'self'", "'unsafe-inline'"]
    img_src: ["'self'", "data:"]
    connect_src: ["'self'"]
    frame_src: ["'self'"]
    sandbox: "allow-scripts allow-forms allow-same-origin"
```

Per-server overrides supported in `ServerSpec.Apps.CSP` (parsed as part of the spec).

## Configuration additions

```yaml
apps:
  csp:
    default_src: ["'self'"]
    sandbox: "allow-scripts"

resources:
  max_bytes_per_read: 10485760  # 10 MB; per-tenant override allowed

list_changed:
  default_mode: stable          # alternative: live
  emit_audit_on_suppress: true
```

Per-tenant overrides:

```yaml
tenants:
  - id: acme
    resources:
      max_bytes_per_read: 52428800   # 50 MB
    list_changed:
      default_mode: live
```

## External APIs

```
GET  /v1/resources                  → list across servers (tenant-scoped)
GET  /v1/resources/{uri}            → read (uri url-encoded)
GET  /v1/prompts                    → list
GET  /v1/prompts/{name}             → get
GET  /v1/apps                       → list ui:// resources
GET  /v1/apps/{uri}/preview         → render (dev mode only; off in prod)
```

Response shapes mirror the MCP shapes with the rewritten URIs.

## Implementation walkthrough

### Step 1: Southbound implementations

`internal/mcp/southbound/stdio/client.go`: implement `ListResources`, `ReadResource`, `ListResourceTemplates`, `ListPrompts`, `GetPrompt` with the same request/response plumbing already used for tools. Methods that the downstream does not advertise return `ErrMethodNotFound`; the aggregator filters those out at fan-out time.

`internal/mcp/southbound/http/client.go`: same.

### Step 2: Namespace rewrite

Implement `namespace/uri.go` and tests. Property-test: for a curated set of original URIs (file://, https://, ui://, custom://), `Restore(Rewrite(s, x)) == (s, x)`.

### Step 3: Resource aggregator

`mcpgw/resources.go`: implement `ListAll`, `Read`, `ListTemplates`. Use `errgroup.Group` for concurrent fan-out; use `time.Tick` + `select` to enforce per-server timeout independent of overall.

Tolerance: a per-server failure logs + audits but doesn't fail the request. The audit record is `resource_list_partial_failure` with `{server_id, error}`.

### Step 4: Prompt aggregator

Same structure, simpler (no URI rewrite, just name prefix).

### Step 5: List-changed mux

`mcpgw/listchanged.go`: subscribe to each southbound `Client.Notifications()` channel; classify `notifications/{tools,resources,prompts}/list_changed`; route per session mode.

Cache invalidation: the aggregator keeps a 60s in-memory cache (see Phase 1) of the per-session aggregated lists. On suppressed list-changed in stable mode, simply invalidate the cache for affected sessions; on a fresh list call, rebuild.

### Step 6: Apps registry + CSP

`internal/apps/registry.go`: registered on every `ResourceAggregator.ListAll` discovery of a `ui://`. Index by canonical URI.

`internal/apps/csp.go`: HTML wrapping uses `golang.org/x/net/html` to parse + inject. For non-HTML `text/*` types, no wrapping is needed (no JS execution surface).

### Step 7: Dispatcher routes

Extend `mcpgw/dispatcher.go`:

```go
case protocol.MethodResourcesList:
    return d.resources.ListAll(ctx, sess, cursorOf(req))
case protocol.MethodResourcesRead:
    return d.resources.Read(ctx, sess, paramsOf(req).URI)
case protocol.MethodResourcesTemplatesList:
    return d.resources.ListTemplates(ctx, sess, cursorOf(req))
case protocol.MethodPromptsList:
    return d.prompts.ListAll(ctx, sess, cursorOf(req))
case protocol.MethodPromptsGet:
    return d.prompts.Get(ctx, sess, paramsOf(req).Name, paramsOf(req).Arguments)
```

Capability advertisement in `initialize`: now turn on `resources` and `prompts` (with `listChanged: true`).

### Step 8: REST API

`internal/server/api/handlers_resources.go`:
- `GET /v1/resources` — tenant-scoped list. Calls `ResourceAggregator.ListAll` with a synthetic in-process session.
- `GET /v1/resources/{uri}` — `uri` is double-URL-decoded; passed to `Read`.

`handlers_prompts.go`, `handlers_apps.go`: similar.

### Step 9: Console

`web/console/src/routes/resources/+page.svelte`: table grouped by server, with size, MIME, last seen. Use the component-library Data Table.
`web/console/src/routes/prompts/+page.svelte`: table with arguments hint.
`web/console/src/routes/apps/+page.svelte`: card layout; in dev mode, each card has a "Preview" button that opens `/v1/apps/{uri}/preview` in a sandboxed iframe.

All UI styling sourced from `web/console/src/lib/tokens.css`. Per-component CSS is allowed only via Svelte scoped styles that reference token variables, never raw colors/spacings.

### Step 10: Audit events

Phase 3 emits the following event types (Phase 5 will own the audit store; Phase 3 calls `audit.Emitter.Emit` which is wired to slog if Phase 5 isn't done yet):
- `resource_list_partial_failure`
- `resource_truncated`
- `list_changed_suppressed`
- `list_changed_forwarded`
- `app_resource_discovered`

## Test plan

### Unit

- `internal/catalog/namespace/uri_test.go`
  - `TestRewriteRestore_FileURI`
  - `TestRewriteRestore_HTTPSURI`
  - `TestRewriteRestore_UIURI`
  - `TestRewriteRestore_CustomScheme` (expects raw/base64url roundtrip)
  - `TestRewriteRestore_PromptName`

- `internal/server/mcpgw/resources_test.go`
  - `TestListAll_TwoServers_Aggregated`
  - `TestListAll_OneServerErrors_Tolerant`
  - `TestRead_RoutesByPrefix`
  - `TestRead_Unknown_PrefixRouting_Returns404`
  - `TestRead_OversizedTruncates` — 50 MB downstream, 1 MB limit, expect truncated content + meta.

- `internal/server/mcpgw/prompts_test.go`
  - `TestListAll_PrefixesNames`
  - `TestGet_StripsPrefixAndRoutes`
  - `TestGet_UnknownPrefix_Errors`

- `internal/apps/registry_test.go`
  - `TestRegister_Idempotent`
  - `TestLookup_Hit`
  - `TestListByTenant_FiltersByPolicy` — placeholder; Phase 5 wires policy filter.

- `internal/apps/csp_test.go`
  - `TestCompose_HasHead_InjectsMeta`
  - `TestCompose_NoHead_CreatesHead`
  - `TestCompose_HeaderValue_AllDirectives`
  - `TestCompose_ReturnsSandboxMeta`

- `internal/server/mcpgw/listchanged_test.go`
  - `TestStableMode_Suppresses_AuditsAndInvalidatesCache`
  - `TestLiveMode_Forwards`
  - `TestMixedSessions_DifferentModes` — two sessions, one stable one live; only live receives.

### Integration

- `test/integration/resources_e2e_test.go`
  - `TestE2E_ResourceListRead_StdioAndHTTPDownstreams`
  - `TestE2E_ResourceTemplates_Substitution`
  - `TestE2E_ListChangedSuppression`
  - `TestE2E_LargeResourceTruncation`

- `test/integration/prompts_e2e_test.go`
  - `TestE2E_PromptListGet`

- `test/integration/apps_e2e_test.go`
  - `TestE2E_UIResourceWithCSP` — downstream returns simple HTML; client receives wrapped HTML with CSP meta tag.
  - `TestE2E_AppsRegistryPopulated`

## Common pitfalls

- **URI rewriting must be idempotent**: aggregator may re-process a list after a cache invalidation; rewriting an already-rewritten URI must not double-prefix. Detect with a sentinel scheme check (`mcp+server://` already prefixed → no-op).
- **Cursor opacity**: clients pass cursors back verbatim. Encode them as opaque base64-URL of internal state. Never embed sensitive info — cursors are user-visible.
- **`text/html` MIME inference**: some downstream servers return `application/octet-stream` for everything. Don't auto-wrap with CSP unless MIME is explicitly `text/html` or extension is `.html`/`.htm`. CSP-wrapping a JSON file would corrupt it.
- **`<head>` injection** with `golang.org/x/net/html`: the parser can produce odd structures for malformed HTML. Always re-render via `html.Render` rather than string manipulation.
- **List-changed timing race**: a notification can arrive between `ListAll` and the cache write. Use a versioned cache: every downstream notification increments a "generation" counter; cache writes check generation matches.
- **Resource subscriptions**: spec allows `resources/subscribe` for live updates per-resource; Phase 3 does NOT implement subscription forwarding to clients. If a client subscribes, return success but don't actually forward (document this in `_meta.portico.subscriptionForwarded: false`). Subscription forwarding is post-V1.
- **CSP `'unsafe-inline'`**: many real MCP Apps use inline scripts/styles. Default policy without `'unsafe-inline'` will break them. Document per-server `apps.csp` overrides as the escape hatch.
- **MIME injection via filename**: an attacker could name a downstream resource `evil.html` and exploit the extension fallback. The MIME inference must trust the downstream's declared MIME first; extension fallback is only when MIME is missing or `application/octet-stream`.

## Out of scope

- Resource subscriptions forwarding (post-V1).
- Real artifact storage for truncated content (Phase 5 skeleton; full post-V1).
- Skill-pack-driven resource exposure (Phase 4 — Phase 4 reuses the aggregator with `skill://` scheme).
- Live MCP Apps interaction (e.g. tool call from inside an iframe). The CSP we emit forbids it by default; opening that channel is post-V1.

## Done definition

1. All acceptance criteria pass.
2. Coverage ≥ 75% for `internal/server/mcpgw`, `internal/apps`, `internal/catalog/namespace`.
3. Three downstream mock servers in integration tests, each with distinct resources, prompts, and at least one `ui://` resource — all aggregated correctly.
4. The Console pages `/resources`, `/prompts`, `/apps` populate live and update on registry changes.
5. Demo flow:
   ```bash
   curl -X POST http://localhost:8080/mcp -d '{"jsonrpc":"2.0","id":1,"method":"resources/list"}'
   curl -X POST http://localhost:8080/mcp -d '{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"mcp+server://github/file/path/to/README.md"}}'
   ```

## Hand-off to Phase 4

Phase 4 inherits aggregators for resources and prompts. Its job: introduce the Skill Pack runtime — a virtual directory of skill files exposed under `skill://` URIs that flows through the *same* aggregator (with `LocalDir` source contributing virtual resources). Skill prompts auto-register via the `prompts` aggregator. The MCP Apps registry from Phase 3 is reused for `binding.ui` references in skill manifests.
