# Phase 5 — Auth, Policy, Credentials, Approval

> Self-contained implementation plan. Builds on Phase 0–4.

## Goal

Turn Portico from "open gateway" into "governed gateway." Phase 5 introduces full credential management, policy enforcement, the approval flow (elicitation + fallback), and the production-grade audit store. Skill Pack `binding.policy` and risk-class metadata become operational. After Phase 5, every tool call goes through:

1. Identity resolution (tenant + user from JWT).
2. Effective catalog filtering (tenant entitlements + tool allowlist + skill enablement).
3. Risk-class evaluation (server defaults + skill overrides).
4. Approval check (elicitation if host supports, else structured error).
5. Credential resolution (per strategy: OAuth token exchange, env injection, header injection, secret reference, credential shim).
6. Tool execution.
7. Audit event with full chain of decisions.

## Why this phase exists

This is the layer enterprises buy. Phase 5 is also where the V1 promise — "credentials live behind the gateway, agents never see them" — becomes real. The approval flow design (host renders, gateway emits) is the keystone for production safety: every destructive operation flows through a confirmable, auditable checkpoint.

## Prerequisites

Phases 0–4 complete. Specifically:
- JWT middleware exists and propagates tenant identity (Phase 0).
- Skill Packs declare `binding.policy.requires_approval` and `risk_classes` (Phase 4).
- `approvals` table exists in SQLite (Phase 0 schema).
- Audit event types are emitted to slog from earlier phases (Phase 5 wires them to the store).
- Northbound transport supports server-initiated requests (`elicitation/create`) over the SSE channel (Phase 1 carries notifications; Phase 5 needs **server-initiated requests** with response correlation — extend slightly).

## Deliverables

1. Real `Vault` implementation backing the file-encrypted store + key rotation hooks (`internal/secrets/`).
2. OAuth 2.0 token exchange flow (RFC 8693) in `internal/secrets/oauth/`.
3. Credential injection strategies in `internal/secrets/inject/`.
4. Policy engine in `internal/policy/`.
5. Approval flow in `internal/policy/approval/` with elicitation + structured-error paths.
6. Audit store under `internal/audit/` (SQLite-backed; replaces Phase 4's slog-only sink).
7. Northbound transport: server-initiated request channel and response correlation.
8. Tool dispatcher integrates the policy → approval → credential pipeline.
9. APIs: `/v1/approvals*`, `/v1/audit/events`, `/v1/admin/secrets/*`.
10. Console: pending approvals page, audit log search, secret refs admin.
11. CLI: `portico vault put|get|delete|list`, `portico vault rotate-key`.
12. Tests: vault crypto, OAuth exchange, policy decision matrix, approval elicitation flow, fallback error, audit completeness, credential injection per strategy.

## Acceptance criteria

1. `portico vault put --tenant acme --name github_token --value <tok>` stores an encrypted value; `portico vault get` returns it. The on-disk vault file is unreadable without the master key.
2. A configured server with `auth.strategy: oauth2_token_exchange` and an `auth.exchange.audience: github` resolves a tenant's broker token into a downstream-scoped token via RFC 8693, caches it for the token's TTL minus 30s, and injects it as `Authorization: Bearer ...` on each southbound HTTP MCP request.
3. A configured stdio server with `auth.strategy: env_inject` and `auth.env: ["GITHUB_TOKEN={{vault:github_token}}"]` receives the value at process spawn, never visible to the agent.
4. A tool call to `github.create_review_comment` (declared `requires_approval` by the active skill pack, `risk_class: external_side_effect`):
   - With elicitation-capable host: gateway sends `elicitation/create` server-initiated request; client presents UI; on approval, tool runs; on denial, returns `policy_denied`.
   - Without elicitation: gateway responds to original `tools/call` with JSON-RPC error `-32001 approval_required` carrying structured payload.
5. An approval times out after 5 minutes (configurable per-tenant); `approvals.status` flips to `expired`; the original call returns `approval_timeout` error.
6. Audit events for every: tool call (start/complete/fail), policy decision, approval pending/decided, credential injection, server health change. Stored in SQLite, queryable via `GET /v1/audit/events` with filters.
7. Tool allowlist/denylist enforcement: a tool not in the allowlist for the tenant returns `policy_denied` with `reason: not_allowed`.
8. Risk classes operational: per-server defaults (`auth.default_risk_class: read`); per-tool overrides via skill `binding.policy.risk_classes`. Risk class informs default approval requirement (`destructive` → always approval; `external_side_effect` → approval; `read` → no approval; `write` → policy-dependent).
9. Credential isolation test: tenant `acme` and tenant `beta` both have a `github_token` in vault; an integration test asserts `acme`'s GitHub server cannot read `beta`'s token even by manipulating env interpolation.
10. Performance: authorization + policy + credential resolution adds ≤ 5ms median (P50) overhead to a tool call vs. baseline (Phase 4).
11. Vault key rotation: `portico vault rotate-key --new-key=<base64>` re-encrypts in place; service restart picks up the new key without data loss.

## Architecture

```
+----------------+
| northbound POST|
+-------+--------+
        |
        v
+----------------------------------+
| Auth + Tenant (Phase 0)          |
+-------+--------------------------+
        |
        v
+----------------------------------+
| Policy Engine (Phase 5)          |
|   resolve allow/deny             |
|   resolve risk class             |
|   compute approval requirement   |
+-------+--------------------------+
        |  if approval required
        v
+----------------------------------+
| Approval Flow                    |
|   elicitation/create OR error    |
|   pending row in approvals table |
+-------+--------------------------+
        |  approved
        v
+----------------------------------+
| Credential Resolver              |
|   pick strategy from server.auth |
|   exchange / inject              |
+-------+--------------------------+
        |
        v
+----------------------------------+
| Dispatcher → southbound (Ph 1)   |
+-------+--------------------------+
        |
        v
+----------------------------------+
| Audit emitter                    |
|   tool_call.{start,complete,fail}|
|   policy.decision                |
|   approval.{pending,decided}     |
|   credential.injected            |
+----------------------------------+
```

## Package layout

```
internal/secrets/
  vault.go              # interface (was stub Phase 2)
  filevault.go          # real impl: AES-256-GCM, on-disk YAML
  filevault_test.go
  oauth/
    exchange.go         # RFC 8693 token exchange
    cache.go            # token cache with TTL
    exchange_test.go
  inject/
    inject.go           # CredentialInjector interface
    env.go              # env_inject strategy
    header.go           # http_header_inject strategy
    shim.go             # credential_shim (stdio, post-Phase-5 stretch)
    secretref.go        # static secret reference
    inject_test.go
internal/policy/
  engine.go             # PolicyEngine
  rules.go              # rule types (allow/deny, risk class, approvals)
  riskclass.go          # constants + helpers
  engine_test.go
  approval/
    flow.go             # ApprovalFlow
    elicit.go           # elicitation request building
    fallback.go         # structured-error mapping
    store.go            # uses approvals table
    flow_test.go
internal/audit/
  emitter.go            # Emitter interface (Phase 4 stub now real)
  store.go              # SQLite-backed
  redact.go             # secret redaction
  emitter_test.go
  store_test.go
internal/mcp/northbound/http/
  server_initiated.go   # server-initiated request + response correlation
internal/server/api/
  handlers_approvals.go
  handlers_audit.go     # real impl (replaces Phase 0 stub)
  handlers_secrets.go   # admin only
cmd/portico/
  cmd_vault.go          # vault put|get|delete|list|rotate-key
web/console/src/routes/
  approvals/+page.svelte
  audit/+page.svelte
  admin/secrets/+page.svelte   # admin scope only
test/integration/
  policy_e2e_test.go
  approval_e2e_test.go
  credential_e2e_test.go
  audit_e2e_test.go
```

## Vault

```go
// internal/secrets/vault.go (re-declared)
type Vault interface {
    Get(ctx context.Context, tenantID, name string) (string, error)
    Put(ctx context.Context, tenantID, name, value string) error
    Delete(ctx context.Context, tenantID, name string) error
    List(ctx context.Context, tenantID string) ([]string, error)
    RotateKey(ctx context.Context, newKey []byte) error
}
```

### File vault implementation

On-disk format (encrypted):

```yaml
# vault.yaml (encrypted)
version: 1
tenants:
  acme:
    github_token: <base64(nonce + ciphertext + tag)>
    aws_access_key: <...>
  beta:
    github_token: <...>
```

Encryption per value:
- `key = HKDF-SHA256(masterKey, info="portico/v1/" + tenant + "/" + name)` — derives a unique key per (tenant, name), so leaking one ciphertext doesn't help with others.
- `nonce = 12-byte random`; `aad = tenant + "/" + name`.
- AES-256-GCM `Seal(nonce, plaintext, aad)`.

The whole `vault.yaml` is then optionally also encrypted at rest (envelope mode); for V1 we keep value-level encryption only — operators are responsible for filesystem permissions.

Master key:
- Read from `PORTICO_VAULT_KEY` env var, base64-encoded 32 bytes.
- If unset and the vault is empty, allow startup (vault is read-only, any get returns "not configured").
- If unset but vault contains data → fail fast with a precise message.

Atomic writes: write to `vault.yaml.tmp`, fsync, rename. Use a process-level mutex; cross-process correctness is post-V1 (assume single Portico instance per vault).

### Key rotation

`RotateKey(newKey)`:
1. Read all values with old key.
2. Re-encrypt with new key (HKDF derives new per-value keys).
3. Write to `vault.yaml.tmp.rotate`, fsync, rename.
4. Update key cache in memory.
5. Operator updates `PORTICO_VAULT_KEY` for next process start; CLI sets a sidecar file `.vault.key.next` that the running process reads via SIGHUP (post-V1; V1 requires restart).

## OAuth token exchange

```go
// internal/secrets/oauth/exchange.go
type ExchangeConfig struct {
    TokenURL       string
    ClientID       string
    ClientSecret   string  // resolved via Vault
    Audience       string
    Scope          string
    GrantType      string  // urn:ietf:params:oauth:grant-type:token-exchange
    SubjectTokenSrc string // "jwt" → use the incoming Bearer
}

type Exchanger struct {
    cfg   ExchangeConfig
    cache *Cache
    http  *http.Client
}

func (e *Exchanger) Exchange(ctx context.Context, tenantID, userID, subjectToken string) (*Token, error)

type Token struct {
    AccessToken string
    TokenType   string
    ExpiresAt   time.Time
    Scope       string
}
```

Cache key: `(tenantID, userID, audience)`. TTL = `expires_in - 30s`. On cache miss, perform exchange; on success, cache + return.

Per RFC 8693:
- Send `POST` to `TokenURL` with `subject_token`, `subject_token_type=urn:ietf:params:oauth:token-type:jwt`, `requested_token_type=urn:ietf:params:oauth:token-type:access_token`, `audience`, `scope`.
- Authentication: client basic auth (client_id + client_secret) by default; configurable to private_key_jwt.
- Errors mapped to typed `*ExchangeError` with retry logic (5xx retryable, 4xx not).

## Credential injectors

```go
// internal/secrets/inject/inject.go
type Injector interface {
    Strategy() string
    Apply(ctx context.Context, req *PrepRequest, target *PrepTarget) error
}

type PrepRequest struct {
    TenantID   string
    UserID     string
    SessionID  string
    SubjectToken string  // raw JWT from incoming request
    ServerSpec *registry.ServerSpec
}

type PrepTarget struct {
    Env     map[string]string  // for stdio
    Headers map[string]string  // for HTTP southbound
}
```

### Strategies

| Strategy           | Lookup                         | Effect                                                                        |
|--------------------|--------------------------------|-------------------------------------------------------------------------------|
| `env_inject`       | Vault                          | Resolves `{{vault:name}}` patterns in `auth.env[]`, sets PrepTarget.Env       |
| `http_header_inject` | Vault                        | Resolves `{{vault:name}}`, sets PrepTarget.Headers                            |
| `secret_reference` | Vault (literal lookup)         | Same as above; just a thinner alias                                           |
| `oauth2_token_exchange` | Exchanger.Exchange         | Result token written to PrepTarget.Headers["Authorization"] = "Bearer …"      |
| `credential_shim`  | Vault (per-call)               | For long-lived stdio: opens a secondary control channel to inject creds       |

The `credential_shim` strategy is **declared in V1 but unimplemented** in Phase 5 — Phase 5 returns `not_yet_supported` if used, with a TODO for post-V1. It's reserved in the spec to avoid breaking changes.

### Server spec extension

```yaml
servers:
  - id: github
    transport: http
    runtime_mode: remote_static
    http:
      url: https://api.githubmcp.example.com/mcp
    auth:
      strategy: oauth2_token_exchange
      default_risk_class: read
      exchange:
        token_url: https://auth.example.com/oauth/token
        client_id: portico-gateway
        client_secret_ref: oauth_client_secret
        audience: github-mcp
        scope: "repo read:org"
        grant_type: urn:ietf:params:oauth:grant-type:token-exchange
        subject_token_src: jwt

  - id: postgres
    transport: stdio
    runtime_mode: per_user
    stdio:
      command: postgres-mcp
    auth:
      strategy: env_inject
      env:
        - "PG_DSN={{vault:pg_dsn}}"
      default_risk_class: write
```

## Policy engine

```go
// internal/policy/engine.go
type Engine struct {
    registry  *registry.Registry
    skills    *runtime.Catalog
    enable    *runtime.Enablement
    cfg       Config
    log       *slog.Logger
}

type Config struct {
    DefaultRiskClass string  // server-level fallback default
}

type Decision struct {
    Allow              bool
    Reason             string  // not_allowed | denied | tool_disabled | tool_not_found | passes
    RequiresApproval   bool
    RiskClass          string
    Tool               string
    SkillID            string  // origin of approval requirement (if any)
    Notes              []string
}

func (e *Engine) EvaluateToolCall(ctx context.Context, tenantID, sessionID, userID, toolName string) (Decision, error)
```

Rule application order (first match wins for "deny"):
1. Tool exists in registered server for tenant. If not → `not_allowed` (`tool_not_found`).
2. Server enabled for tenant. If not → `not_allowed`.
3. Tenant denylist contains tool → `denied`.
4. Tenant allowlist exists AND tool not in it → `not_allowed`.
5. Tool gated by an enabled skill that required-tools it: collect the tightest risk_class (skill override > server default > config default).
6. Compute `RequiresApproval`:
   - If risk_class ∈ {`destructive`, `external_side_effect`, `sensitive_read`}: true (default).
   - Else if any active skill lists it in `policy.requires_approval`: true.
   - Else: false.
7. Allow.

Tenant-level allow/deny:

```yaml
tenants:
  - id: acme
    policy:
      tool_allowlist: []                # empty = allow all (subject to per-server)
      tool_denylist: [github.delete_*]  # globs
      approval_timeout: 5m
```

## Approval flow

```go
// internal/policy/approval/flow.go
type Flow struct {
    store    *Store
    sessions *mcpgw.SessionRegistry
    audit    audit.Emitter
    cfg      Config
    log      *slog.Logger
}

type Config struct {
    DefaultTimeout time.Duration  // 5m
}

type Outcome struct {
    Approved bool
    Reason   string  // approved | denied | timeout | error
    Decision *Approval
}

func (f *Flow) Run(ctx context.Context, sess *mcpgw.Session, dec policy.Decision, params protocol.CallToolParams) (Outcome, error)
```

`Run` flow:
1. Persist `Approval{status: pending, expires_at: now + timeout}`.
2. Emit `approval.pending` audit event.
3. If `sess.ClientCaps.HasElicitation`:
   - Build `elicitation/create` params (see below).
   - Send via northbound server-initiated request channel (Phase 5 transport addition); wait for response.
   - On `accept` → mark approved, emit `approval.decided`, return.
   - On `reject` → mark denied, emit, return.
   - On timeout → mark expired, emit, return.
4. Else (no elicitation):
   - Return `Outcome{Approved: false, Reason: "fallback_required"}` so the dispatcher sends a JSON-RPC error `-32001 approval_required` with structured payload.
   - The pending approval row stays open (status pending) — operators or external systems can resolve via `POST /v1/approvals/{id}/{approve|deny}`. This enables out-of-band human approval flows even in non-elicitation hosts; on resolution, the *next* `tools/call` for the same tool with the same args within a configurable replay window (default 60s) treats the pre-approved decision as binding. Phase 5 implements the storage; the matching/replay is documented as a "manual retry" pattern; full async-approval is post-V1.

### Elicitation request shape

Per MCP spec, `elicitation/create` is a server-initiated request that asks the client to render a UI for input. We use it for boolean approval:

```json
{
  "jsonrpc": "2.0",
  "id": 1024,
  "method": "elicitation/create",
  "params": {
    "message": "Approve calling github.create_review_comment? Risk: external_side_effect.",
    "requestedSchema": {
      "type": "object",
      "properties": {
        "approve": {"type":"boolean", "title":"Approve this action?"},
        "note":    {"type":"string",  "title":"Optional reason"}
      },
      "required": ["approve"]
    },
    "_meta": {
      "portico": {
        "approval_id": "01HE...",
        "tool": "github.create_review_comment",
        "risk_class": "external_side_effect",
        "skill_id": "github.code-review",
        "args_summary": "{\"owner\":\"acme\",\"repo\":\"web\",\"pr\":42,\"body\":\"…\"}",
        "expires_at": "2026-05-05T17:35:00Z"
      }
    }
  }
}
```

The client returns `{"approve": true|false, "note": "…"}`. On `false`, the call is denied with `policy_denied` and reason `user_denied`.

### Fallback structured error

Returned as JSON-RPC error to the original `tools/call`:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "error": {
    "code": -32001,
    "message": "approval_required",
    "data": {
      "tool": "github.create_review_comment",
      "risk_class": "external_side_effect",
      "skill_id": "github.code-review",
      "approval_id": "01HE...",
      "approval_status_url": "https://gateway.example.com/v1/approvals/01HE...",
      "expires_at": "2026-05-05T17:35:00Z",
      "args_summary": {"owner":"acme","repo":"web","pr":42}
    }
  }
}
```

## Server-initiated requests in northbound transport

Phase 1 northbound handles client-initiated requests; Phase 5 needs the server to initiate `elicitation/create` and receive a response.

Mechanism:
- Each session has a stable SSE channel (Phase 1 already opens one for notifications).
- Server-initiated requests are written as SSE events with `event: server_request` and a JSON-RPC envelope.
- The client responds via `POST /mcp` with `Mcp-Session-Id` header and a JSON-RPC response carrying the matching ID.
- The dispatcher maintains a map `serverRequestID -> chan *protocol.Response` and a 5-minute TTL cleanup goroutine.

```go
// internal/mcp/northbound/http/server_initiated.go
type ServerInitiatedRequester struct {
    sessions *mcpgw.SessionRegistry
    pending  sync.Map  // requestID -> *pendingReq
}

type pendingReq struct {
    sessionID string
    resp      chan *protocol.Response
    expiresAt time.Time
}

func (s *ServerInitiatedRequester) Send(ctx context.Context, sess *mcpgw.Session, method string, params any) (*protocol.Response, error)
```

`Send` blocks until response received or ctx cancelled. Response delivery happens when the POST handler sees an envelope whose ID matches a pending entry.

If the SSE connection drops before response, the pending entry expires after 5m and `Send` returns `error: stream_disconnected`.

## Audit store

```go
// internal/audit/emitter.go
type Emitter interface {
    Emit(ctx context.Context, e Event)
}

type Event struct {
    Type        string
    TenantID    string
    SessionID   string
    UserID      string
    OccurredAt  time.Time
    TraceID     string
    SpanID      string
    Payload     map[string]any
}

type FanoutEmitter struct {
    sinks []Emitter
}
```

```go
// internal/audit/store.go
type Store struct {
    db        *sqlite.DB
    redactor  *Redactor
    log       *slog.Logger
    buffer    chan Event   // bounded; spillover drops oldest with metric
}

func (s *Store) Emit(ctx context.Context, e Event)
func (s *Store) Query(ctx context.Context, q Query) ([]*Event, string, error)
```

The store buffers events into a channel; a worker batches inserts every 200ms (or 100 events). On overflow, drop oldest with `audit.dropped` event recorded. Synchronous emit is available via `EmitSync` for tests.

```go
// internal/audit/redact.go
type Redactor struct {
    patterns []*regexp.Regexp
}

func NewDefault() *Redactor // built-in patterns: bearer tokens, basic auth, AWS keys, GitHub PATs, Slack tokens, generic JWT
func (r *Redactor) Redact(payload map[string]any) map[string]any
```

Redaction runs on every event before persistence. Test corpus of known token shapes ensures redaction is consistent.

## Event types (Phase 5)

| Type                          | Emitted by                | Payload keys                                      |
|-------------------------------|---------------------------|---------------------------------------------------|
| `tool_call.start`             | dispatcher                | tool, server_id, args_summary, skill_id           |
| `tool_call.complete`          | dispatcher                | duration_ms, result_size_bytes                    |
| `tool_call.failed`            | dispatcher                | duration_ms, error_code, error_message            |
| `policy.allowed`              | policy engine             | tool, risk_class, requires_approval, decision    |
| `policy.denied`               | policy engine             | tool, reason                                      |
| `approval.pending`            | approval flow             | approval_id, tool, risk_class                     |
| `approval.decided`            | approval flow             | approval_id, decision, decided_by                 |
| `approval.expired`            | approval flow             | approval_id                                       |
| `credential.injected`         | injector                  | strategy, server_id, scope (no values)            |
| `credential.exchange.success` | oauth exchanger           | audience, ttl_s                                   |
| `credential.exchange.failed`  | oauth exchanger           | audience, error_code                              |
| `vault.get`                   | vault                     | name, hit (no value)                              |
| `vault.put`                   | vault (admin)             | name                                              |
| `vault.delete`                | vault (admin)             | name                                              |
| `audit.dropped`               | store buffer overflow     | dropped_count                                     |

Plus existing event types from earlier phases now persisted: server health changes, registry changes, list-changed events.

## External APIs

```
GET    /v1/audit/events
       Query: ?type=tool_call.complete&since=...&limit=100&cursor=...
       → 200 {"events":[...], "next_cursor":""}

GET    /v1/approvals
       Query: ?status=pending&since=...
       → 200 [{approval}]

GET    /v1/approvals/{id}
       → 200 {approval}

POST   /v1/approvals/{id}/approve
       Body: {"note":"..."}
       → 200 (operator/admin manual approval; emits approval.decided)

POST   /v1/approvals/{id}/deny
       → 200

# Admin only
GET    /v1/admin/secrets
       → 200 [{tenant_id, name}]                  (no values)

PUT    /v1/admin/secrets/{tenant}/{name}
       Body: {"value":"..."}
       → 204

DELETE /v1/admin/secrets/{tenant}/{name}
       → 204
```

## CLI

```
portico vault put    --tenant <id> --name <key> [--value <v> | --from-file <path> | --from-stdin]
portico vault get    --tenant <id> --name <key>            (admin local; stderr warns about plaintext print)
portico vault delete --tenant <id> --name <key>
portico vault list   --tenant <id>
portico vault rotate-key --new-key <base64>                (re-encrypts; warns to update PORTICO_VAULT_KEY)
```

## Implementation walkthrough

### Step 1: Vault

Implement `FileVault` per spec. Tests use `t.TempDir()` and a fixed test key.

### Step 2: OAuth exchanger

Implement `Exchange` against a `httptest.Server` that mimics RFC 8693. Cache keyed on (tenant, user, audience).

### Step 3: Injectors

Each strategy is a small file. Keep `Apply` pure: takes a request, mutates target. The dispatcher composes them in order from `server.auth.strategies` (V1 supports a single strategy per server; future-proof the API).

### Step 4: Policy engine

Pull rules from: server registry (default risk class), skill catalog (per-tool overrides + requires_approval), tenant config (allow/deny). Compute Decision in one pass.

### Step 5: Server-initiated requests

Extend the northbound transport. SSE writer must be safe for concurrent calls (use a mutex). Pending request map sized for ~1k concurrent server requests.

### Step 6: Approval flow

Implement `Run`. Test paths: elicitation accepted, elicitation denied, elicitation timeout, no-elicitation → fallback error, manual approve via API.

### Step 7: Audit store

Buffered worker. Drop-oldest semantics. Tests assert ordering preserved on serial inserts and that high-volume bursts don't deadlock.

### Step 8: Dispatcher integration

Modify `mcpgw/dispatcher.go::handleToolsCall`:

```go
func (d *Dispatcher) handleToolsCall(ctx, sess, req) {
    params := parse(req)
    server, tool := namespace.Split(params.Name)

    // 1. Policy
    dec, err := d.policy.EvaluateToolCall(ctx, sess.TenantID, sess.ID, sess.UserID, params.Name)
    if err != nil { ... }
    if !dec.Allow {
        return policyDeniedResponse(dec)
    }

    // 2. Approval (if required)
    if dec.RequiresApproval {
        outcome, err := d.approval.Run(ctx, sess, dec, params)
        if err != nil { ... }
        if outcome.Reason == "fallback_required" {
            return approvalRequiredErrorResponse(dec, outcome.Decision)
        }
        if !outcome.Approved {
            return policyDeniedResponse(decWithReason("user_denied"))
        }
    }

    // 3. Resolve credentials and southbound client
    spec := d.registry.Effective(sess.TenantID, server)
    target := &inject.PrepTarget{...}
    if spec.Auth.Strategy != "" {
        injector := d.injectors[spec.Auth.Strategy]
        if err := injector.Apply(ctx, &inject.PrepRequest{...}, target); err != nil { ... }
    }
    client, err := d.sup.AcquireWith(ctx, key, spec, target)

    // 4. Call + audit
    d.audit.Emit(ctx, audit.Event{Type:"tool_call.start", ...})
    res, err := client.CallTool(ctx, tool, params.Arguments, progressCb)
    if err != nil { d.audit.Emit("tool_call.failed", ...); return }
    d.audit.Emit("tool_call.complete", ...)
    return res
}
```

### Step 9: Console

`web/console/src/routes/approvals/+page.svelte`: pending list with approve/deny buttons (admin scope only); polls `/v1/approvals?status=pending` every 2s via a Svelte store.
`web/console/src/routes/audit/+page.svelte`: search box, type filter, time range; cursor-paginated table of 50 events per page (component-library Pagination).
`web/console/src/routes/admin/secrets/+page.svelte`: tenant/name list; "Add" form (admin only); never displays values. Uses component-library Form primitives; tokens for spacing.

## Test plan

### Unit

- `internal/secrets/filevault_test.go`
  - `TestPutGet_Roundtrip`
  - `TestPutGet_TenantIsolation` — get tenant A's value with tenant B's name fails or returns empty.
  - `TestPutGet_WrongMasterKey` — decrypt with wrong key fails.
  - `TestRotateKey_PreservesValues`
  - `TestStartup_NoKeyAndEmpty_OK`
  - `TestStartup_NoKeyAndNonEmpty_Fails`

- `internal/secrets/oauth/exchange_test.go`
  - `TestExchange_Success`
  - `TestExchange_Cached`
  - `TestExchange_4xxError_NotRetried`
  - `TestExchange_5xxError_RetriedAndCached`

- `internal/secrets/inject/inject_test.go`
  - `TestEnvInject_VaultLookup`
  - `TestHeaderInject_BearerFormat`
  - `TestSecretRef_LiteralLookup`
  - `TestOAuthExchange_HeaderInjection`
  - `TestUnsupported_credential_shim_Returns_NotImplemented`

- `internal/policy/engine_test.go`
  - `TestEvaluate_AllowDefault`
  - `TestEvaluate_DenyByList`
  - `TestEvaluate_AllowlistMiss`
  - `TestEvaluate_RiskClassFromSkillOverride`
  - `TestEvaluate_DestructiveRequiresApproval_AlwaysOn`
  - `TestEvaluate_RequiresApprovalFromSkillBinding`
  - `TestEvaluate_ToolNotInRegistry_NotFound`

- `internal/policy/approval/flow_test.go`
  - `TestFlow_Elicit_Approved`
  - `TestFlow_Elicit_Denied`
  - `TestFlow_Elicit_Timeout`
  - `TestFlow_NoElicit_FallbackError`
  - `TestFlow_ManualApproveViaAPI`

- `internal/audit/store_test.go`
  - `TestEmitQuery_Roundtrip`
  - `TestRedaction_StripsBearerTokens`
  - `TestBufferOverflow_DropsOldest_RecordsAuditDropped`
  - `TestQuery_TenantScoped`
  - `TestQuery_TypeFilter_TimeRange_Cursor`

- `internal/mcp/northbound/http/server_initiated_test.go`
  - `TestSend_RequestRoundtrip`
  - `TestSend_StreamDropped_ReturnsError`
  - `TestSend_TimeoutCleansUp`

### Integration

- `test/integration/policy_e2e_test.go`
  - `TestE2E_DenyByList`
  - `TestE2E_DestructiveAlwaysApproves` — without skill, `destructive` risk class still triggers approval.
  - `TestE2E_TenantAllowlistEnforced`

- `test/integration/approval_e2e_test.go`
  - `TestE2E_ElicitApproveFlow` — gateway sends elicitation; mock client approves; tool runs.
  - `TestE2E_ElicitDeny` — denial returns policy_denied.
  - `TestE2E_FallbackError_NoElicitation` — client without elicitation cap; expect -32001.
  - `TestE2E_FallbackThenManualApprove` — operator approves via API; document-only retry pattern.

- `test/integration/credential_e2e_test.go`
  - `TestE2E_OAuthExchange_HeaderInjected`
  - `TestE2E_EnvInject_StdioServerReceives`
  - `TestE2E_TenantIsolation_OnVault` — two tenants with same key name; expect different values delivered.
  - `TestE2E_VaultRotateKey_ServiceRestart` — rotate, restart, all values readable.

- `test/integration/audit_e2e_test.go`
  - `TestE2E_FullToolCall_GeneratesEvents` — assert presence of start/complete + policy.allowed + credential.injected.
  - `TestE2E_RedactionInPersistedRows`

## Common pitfalls

- **JWT subject token forwarding**: only use the *raw* incoming JWT as the OAuth subject token if you trust the issuer for the audience. Document explicitly that operators must configure the OAuth IdP's policy to *also* validate the subject token's audience and issuer.
- **Time-of-check vs time-of-use**: between policy evaluation and tool execution, registry/skill state can change. Snapshot the decision into the request context; do not re-evaluate mid-call.
- **Server-initiated request ID space**: must not collide with client-initiated request IDs. Use a separate ID generator (e.g. prefix `s_` or use UUID).
- **Approval re-entry**: if a session has 5 pending approvals, the SSE channel must serialize `elicitation/create` and not interleave responses. Use a per-session mutex around send-and-await.
- **Vault HKDF info string**: include version (`portico/v1/`) so future re-derivation paths can be added without breaking existing values.
- **Redaction false negatives**: regex-based redaction will miss novel formats. Add a content-type check: never log full tool call results raw; truncate to a configurable byte cap (default 4KB) before redaction.
- **Audit buffer**: use `select` with default to drop, not block. Blocking on audit kills throughput under burst.
- **Manual approval via API security**: only `admin` scope can approve via `POST /v1/approvals/{id}/approve`. Audit who approved.
- **OAuth client_secret**: store via `client_secret_ref` pointing into vault, not in plain config.

## Out of scope

- Async approval channels (Slack, email): post-V1.
- Per-tool quotas / rate limiting: post-V1.
- Anomaly detection on audit stream: post-V1.
- Cryptographic signing of audit events for tamper-evidence: post-V1.
- mTLS or alternative northbound auth backends: post-V1.
- Postgres-backed audit at production scale: post-V1.

## Done definition

1. All acceptance criteria pass.
2. Coverage ≥ 80% for `internal/secrets`, `internal/policy`, `internal/audit`.
3. All audit events from earlier phases now persist via the real store; no remaining slog-only paths.
4. Console approval/audit/secrets pages functional.
5. End-to-end demo: a real GitHub MCP server configured with OAuth exchange; an MCP client with elicitation calls `github.create_review_comment`; gateway sends elicitation; client approves; comment posts; audit log shows full chain.

## Hand-off to Phase 6

Phase 6 inherits the full policy, credential, and audit pipeline. Its job: make the catalog stable per session via persisted snapshots, add schema-fingerprint drift detection, wire OpenTelemetry tracing across all layers, and deepen the session inspector UI to show the complete picture (registry → policy → approval → credentials → tool call) in real time.
