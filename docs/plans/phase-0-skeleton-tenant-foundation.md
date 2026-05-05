# Phase 0 — Skeleton & Tenant Foundation

> Self-contained implementation plan. Implementor should be able to start with this document, the RFC, and an empty repo.
>
> **Implementation deviations (recorded after the fact):**
>
> - **Frontend stack pivoted to SvelteKit.** The original plan described "htmx + Templ"; the canonical decision is now **SvelteKit + `@sveltejs/adapter-static`** with the build output (`web/console/build/`) embedded into the Go binary via `//go:embed` and served by the same HTTP listener that handles REST + MCP. Phase 0 ships *transitional* placeholder pages rendered by stdlib `html/template` (`internal/server/ui/templates/*.html`) so the binary boots and the Phase 0 smoke check passes immediately. **Phase 2 owns the SvelteKit migration**: scaffolds `web/console/`, wires the build into the Makefile, replaces the html/template handler with an embed-served SPA, and ships the first real page (Servers). The package comment in `internal/server/ui/handlers.go` records the transition.
> - **Storage protocol layer.** The Phase 0 plan described `internal/storage/sqlite/` directly; the implementation puts it behind an `ifaces.Backend` interface with a factory in `internal/storage/storage.go` and self-registration in the SQLite driver. This is the canonical "easy seam" pattern documented in AGENTS.md §4.4 and applies to every later subsystem with potential alternate backends. Callers (the CLI) blank-import the driver to register it; production code talks to `ifaces.Backend` only.

## Goal

Lay down the project skeleton, tenant foundation, auth middleware, config loader, SQLite store, and Console shell. After Phase 0, `portico dev` starts a working multi-tenant-capable server with health endpoints, JWT auth (dev-mode bypass), and an empty registry; `portico serve` does the same with full JWT auth; and the codebase is ready for Phase 1 to drop in the MCP gateway core.

## Why this phase exists

Multi-tenant from V1 means tenant context must permeate every internal API. Establishing it now — before any code that handles MCP messages or skills — guarantees every later component is built tenant-aware by default. Same logic for storage keying and auth.

## Prerequisites

- Empty repo at `~/Repos/Portico_gateway` with `RFC-001-Portico.md`.
- Go 1.22+ installed.
- `gh` CLI with the `hurtener` account active (for the eventual push).

## Deliverables

1. Go module initialized at `github.com/hurtener/Portico_gateway`.
2. Directory tree per RFC §18.
3. `cmd/portico/main.go` with subcommands: `serve`, `dev`, `validate`, `version`.
4. YAML config loader with hot reload.
5. Tenant context primitive + JWT middleware (dev-mode bypass).
6. HTTP server using `chi` with middleware chain.
7. SQLite schema + migrations for V1 tables (most empty in Phase 0; populated in later phases).
8. Console shell. **Canonical path: a SvelteKit project at `web/console/` built with `@sveltejs/adapter-static` and embedded into the binary via `//go:embed`.** Phase 0 ships a *transitional* placeholder using stdlib `html/template` so the binary boots and the smoke check passes today; Phase 2 scaffolds the SvelteKit project, wires the build into the Makefile, and replaces the html/template handler with an embed-served SPA.
9. `log/slog` JSON logger wired through every package.
10. Makefile + Dockerfile + GitHub Actions CI.
11. README skeleton.
12. Tests: config parsing, JWT validation, dev-mode bypass, tenant context propagation, schema migrations, health endpoints, hot reload.

## Acceptance criteria

A reviewer can confirm Phase 0 is complete by running:

1. `make build` produces `./bin/portico` (single static binary, no CGo).
2. `./bin/portico dev` binds to `127.0.0.1:8080`, logs `listening` JSON line with `tenant_id=dev`, creates `./portico.db` with all expected tables, opens to a Console homepage at `http://localhost:8080/`.
3. `curl http://localhost:8080/healthz` returns `200 OK` with body `{"status":"ok"}`.
4. `curl http://localhost:8080/v1/audit/events` (no Authorization header, dev mode) returns `200 OK` with body `{"events":[]}`.
5. `./bin/portico serve --config testdata/portico.yaml` (a fixture with two tenants and a fake JWKS) starts and rejects `/v1/audit/events` without a JWT (`401`), accepts a JWT for tenant `acme` (`200`, empty list), and refuses to leak tenant `beta` events when queried with the `acme` JWT.
6. `./bin/portico validate --config testdata/portico-bad.yaml` prints a structured error pointing at the offending YAML key and exits non-zero.
7. `go test ./...` passes; coverage ≥ 70% for `internal/config`, `internal/auth`, `internal/storage/sqlite`.
8. Hot reload: writing a new tenant into `portico.yaml` while the server runs causes the next request from a JWT for that tenant to succeed (verified by integration test).

## Architecture

```
+--------------------------+
| cmd/portico              |  CLI entry; wires everything
+------------+-------------+
             |
             v
+--------------------------+
| internal/config          |  YAML loader, hot reload, validation
+------------+-------------+
             |
             v
+--------------------------+      +-----------------------+
| internal/storage/sqlite  |<-----| internal/storage/ifaces|
+------------+-------------+      +-----------------------+
             |
             v
+--------------------------+
| internal/auth/jwt        |  JWT validation (golang-jwt)
+------------+-------------+
             |
             v
+--------------------------+
| internal/auth/tenant     |  TenantContext + middleware
+------------+-------------+
             |
             v
+--------------------------+
| internal/server/api      |  chi router + middleware + placeholder routes
+------------+-------------+
             |
             v
+--------------------------+
| internal/server/ui       |  Console handlers; embeds web/console/build/ in V1.
|                          |  Phase 0 transitional: stdlib html/template.
+--------------------------+
```

## Package layout (created in this phase)

```
cmd/portico/
  main.go
  cmd_serve.go
  cmd_dev.go
  cmd_validate.go
  cmd_version.go
internal/
  config/
    config.go             # types
    loader.go             # parse + validate
    watcher.go            # hot reload via fsnotify
    config_test.go
  auth/
    jwt/
      validator.go
      jwks.go
      validator_test.go
    tenant/
      context.go
      middleware.go
      middleware_test.go
    scope/
      scope.go
  storage/
    ifaces/
      tenant_store.go
      audit_store.go
    sqlite/
      sqlite.go
      migrations.go
      migrations/
        0001_init.sql
      tenant_store.go
      audit_store.go
      sqlite_test.go
  server/
    api/
      router.go
      middleware.go
      handlers_health.go
      handlers_audit.go    # stub returning empty list
      handlers_tenants.go  # admin only, stub
    ui/
      handlers.go
      templates_embed.go
  telemetry/
    logger.go              # slog setup
web/
  console/                 # In V1 this is a SvelteKit project (adapter-static).
                            # Phase 0 ships transitional html/template only:
    static/
      htmx.min.js          # placeholder (no real JS in Phase 0)
      portico.css
    templates/             # Phase 0 transitional only — replaced in Phase 2:
      layout.html
      home.html
      servers.html
      skills.html
      sessions.html
    # Phase 2 scaffolds:
    #   package.json, svelte.config.js, vite.config.ts, tsconfig.json
    #   src/app.html
    #   src/lib/tokens.css           # design tokens — single swap point
    #   src/lib/api.ts                # typed REST client
    #   src/routes/+layout.svelte
    #   src/routes/+page.svelte       # /
    #   src/routes/servers/+page.svelte
    #   src/routes/skills/+page.svelte
    #   src/routes/sessions/+page.svelte
    #   build/                        # generated; embedded by Go binary
go.mod
go.sum
Makefile
Dockerfile
.github/
  workflows/
    ci.yml
README.md
testdata/
  portico.yaml             # valid two-tenant fixture
  portico-bad.yaml         # invalid fixture
  jwks-test.json           # static JWKS for tests
```

## Public types and interfaces

### Config

```go
// internal/config/config.go
package config

import "time"

type Config struct {
    Server  ServerConfig   `yaml:"server"`
    Auth    *AuthConfig    `yaml:"auth,omitempty"`     // nil => dev mode
    Storage StorageConfig  `yaml:"storage"`
    Tenants []TenantConfig `yaml:"tenants"`
    Skills  SkillsConfig   `yaml:"skills"`
    Logging LoggingConfig  `yaml:"logging"`
    // Servers and Registry populated in Phase 2; field present here for forward-compat.
    Servers []ServerSpec   `yaml:"servers,omitempty"`
}

type ServerConfig struct {
    Bind             string        `yaml:"bind"`              // e.g. "0.0.0.0:8080"
    DevModeAutoBind  bool          `yaml:"dev_mode_auto_bind"` // if bind starts with 127.0.0.1, auto dev mode
    ShutdownGrace    time.Duration `yaml:"shutdown_grace"`
}

type AuthConfig struct {
    JWT JWTConfig `yaml:"jwt"`
}

type JWTConfig struct {
    Issuer        string   `yaml:"issuer"`
    Audiences     []string `yaml:"audiences"`
    JWKSURL       string   `yaml:"jwks_url,omitempty"`
    StaticJWKS    string   `yaml:"static_jwks,omitempty"`     // path to local JWKS file
    TenantClaim   string   `yaml:"tenant_claim,omitempty"`    // default: "tenant"
    ScopeClaim    string   `yaml:"scope_claim,omitempty"`     // default: "scope"
    RequiredScope string   `yaml:"required_scope,omitempty"`  // optional global scope check
    ClockSkew     time.Duration `yaml:"clock_skew,omitempty"` // default: 60s
}

type StorageConfig struct {
    Driver string `yaml:"driver"`           // "sqlite" only in Phase 0
    DSN    string `yaml:"dsn"`              // e.g. "file:./portico.db?cache=shared"
}

type TenantConfig struct {
    ID             string            `yaml:"id"`
    DisplayName    string            `yaml:"display_name"`
    Plan           string            `yaml:"plan"`           // free|pro|enterprise
    CredentialsRef string            `yaml:"credentials_ref,omitempty"`
    Entitlements   Entitlements      `yaml:"entitlements"`
    Metadata       map[string]string `yaml:"metadata,omitempty"`
}

type Entitlements struct {
    Skills      []string `yaml:"skills"`        // glob patterns: "github.*", "postgres.sql-*"
    MaxSessions int      `yaml:"max_sessions"`
}

type SkillsConfig struct {
    Sources []SkillSourceConfig `yaml:"sources"`
}

type SkillSourceConfig struct {
    Type string `yaml:"type"` // Phase 0 supports "local"
    Path string `yaml:"path,omitempty"`
}

type LoggingConfig struct {
    Level  string `yaml:"level"`  // debug|info|warn|error
    Format string `yaml:"format"` // json|text
}

type ServerSpec struct {
    // Phase 2 owns this. Phase 0 just parses + ignores it.
    ID          string `yaml:"id"`
    Transport   string `yaml:"transport"`
    RuntimeMode string `yaml:"runtime_mode"`
    // ... (see Phase 2)
}
```

### Loader

```go
// internal/config/loader.go
package config

func Load(path string) (*Config, error)
func (c *Config) Validate() error
func (c *Config) IsDevMode() bool
// IsDevMode returns true iff Auth == nil AND Server.Bind starts with "127.0.0.1" or "localhost".
```

### Hot reload

```go
// internal/config/watcher.go
package config

type ChangeHandler func(old, new *Config) error

type Watcher struct { /* ... */ }

func NewWatcher(path string, current *Config, handler ChangeHandler, log *slog.Logger) (*Watcher, error)
func (w *Watcher) Start(ctx context.Context) error  // blocks until ctx cancel
func (w *Watcher) Stop() error
```

Behavior: only certain fields are hot-reloadable in Phase 0 — `tenants`, `logging.level`. Other field changes log a warning and are applied on next restart. Phase 2 extends this to `servers`.

### Tenant context

```go
// internal/auth/tenant/context.go
package tenant

import "context"

type Identity struct {
    TenantID  string
    UserID    string
    Plan      string
    Scopes    []string
    Issuer    string
    Subject   string
    DevMode   bool
}

type ctxKey struct{}

func With(ctx context.Context, id Identity) context.Context
func From(ctx context.Context) (Identity, bool)
func MustFrom(ctx context.Context) Identity  // panics if missing; for handlers known to be authed
```

### JWT validator

```go
// internal/auth/jwt/validator.go
package jwt

type Validator struct { /* ... */ }

func NewValidator(cfg config.JWTConfig) (*Validator, error)
func (v *Validator) Validate(rawToken string) (*Claims, error)

type Claims struct {
    Subject  string
    Issuer   string
    Audience []string
    Tenant   string
    Plan     string   // optional
    Scopes   []string
    ExpiresAt time.Time
    IssuedAt  time.Time
    Raw      map[string]any  // for downstream extension
}
```

JWKS strategy:
- If `StaticJWKS` is set, load once from disk, parse, cache in-memory.
- If `JWKSURL` is set, fetch via HTTP every 10 minutes; cache; honor `Cache-Control` if present; on fetch failure within first 60s of startup, fail fast; later failures keep serving from cached keys with a warning log.

Algorithm allowlist: RS256, RS384, RS512, ES256, ES384, ES512. HS* algorithms must be rejected (avoid symmetric-key shared secret class of bug).

Clock skew default 60s.

### Auth middleware

```go
// internal/auth/tenant/middleware.go
package tenant

func Middleware(v *jwt.Validator, devMode bool, devTenant string, store TenantStore, log *slog.Logger) func(http.Handler) http.Handler
```

Behavior:
- Path `/healthz`, `/readyz` always pass (no auth).
- Static asset paths (`/static/*`) pass.
- If `devMode == true`, inject Identity{TenantID: devTenant, UserID: "dev", Plan: "enterprise", Scopes: []string{"admin"}, DevMode: true} and continue. Skip JWT entirely.
- Else, extract `Authorization: Bearer <token>`, run `v.Validate`, look up tenant in store; if missing, return 401 `{"error":"unknown_tenant"}`. Inject Identity from claims.
- 401 responses include `WWW-Authenticate: Bearer realm="portico"`.

### Scope helpers

```go
// internal/auth/scope/scope.go
package scope

func Has(id tenant.Identity, scope string) bool
func RequireScope(scope string) func(http.Handler) http.Handler
// Use case: RequireScope("admin") for /v1/admin/*
```

### Storage interfaces

```go
// internal/storage/ifaces/tenant_store.go
package ifaces

type TenantStore interface {
    Get(ctx context.Context, id string) (*Tenant, error)
    List(ctx context.Context) ([]*Tenant, error)
    Upsert(ctx context.Context, t *Tenant) error
    Delete(ctx context.Context, id string) error
}

type Tenant struct {
    ID          string
    DisplayName string
    Plan        string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

```go
// internal/storage/ifaces/audit_store.go
package ifaces

type AuditStore interface {
    Append(ctx context.Context, e *AuditEvent) error
    Query(ctx context.Context, q AuditQuery) ([]*AuditEvent, string, error) // events, next cursor
}

type AuditEvent struct {
    ID          string         // ULID
    TenantID    string
    Type        string
    SessionID   string         // optional
    UserID      string         // optional
    OccurredAt  time.Time
    TraceID     string
    SpanID      string
    Payload     map[string]any
}

type AuditQuery struct {
    TenantID string  // required (admin can pass empty + admin scope)
    Types    []string
    Since    time.Time
    Until    time.Time
    Limit    int
    Cursor   string
}
```

Phase 0 ships stub implementations of these for SQLite. The audit store is used by handlers from Phase 5; Phase 0 just exposes an empty `Query`.

## SQLite schema (V1 baseline)

`internal/storage/sqlite/migrations/0001_init.sql`:

```sql
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version  INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS tenants (
    id            TEXT PRIMARY KEY,
    display_name  TEXT NOT NULL,
    plan          TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Forward-compat: tables Phase 1-6 will populate. Defined now so a single migration sets up V1.
CREATE TABLE IF NOT EXISTS servers (
    tenant_id    TEXT NOT NULL,
    id           TEXT NOT NULL,
    spec_json    TEXT NOT NULL,
    enabled      INTEGER NOT NULL DEFAULT 1,
    schema_hash  TEXT,
    last_error   TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    user_id       TEXT,
    snapshot_id   TEXT,
    started_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    ended_at      TEXT,
    metadata_json TEXT,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON sessions(tenant_id, started_at DESC);

CREATE TABLE IF NOT EXISTS catalog_snapshots (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    session_id    TEXT,
    payload_json  TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_snapshots_tenant ON catalog_snapshots(tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS skill_enablement (
    tenant_id   TEXT NOT NULL,
    session_id  TEXT,                -- NULL means tenant-wide
    skill_id    TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    enabled_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (tenant_id, COALESCE(session_id,''), skill_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS approvals (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    session_id    TEXT NOT NULL,
    user_id       TEXT,
    tool          TEXT NOT NULL,
    args_summary  TEXT,
    risk_class    TEXT,
    status        TEXT NOT NULL,    -- pending|approved|denied|expired
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    decided_at    TEXT,
    expires_at    TEXT NOT NULL,
    metadata_json TEXT,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_approvals_tenant_status ON approvals(tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_events (
    id            TEXT PRIMARY KEY,    -- ULID
    tenant_id     TEXT NOT NULL,
    type          TEXT NOT NULL,
    session_id    TEXT,
    user_id       TEXT,
    occurred_at   TEXT NOT NULL,
    trace_id      TEXT,
    span_id       TEXT,
    payload_json  TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_events(tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_type ON audit_events(tenant_id, type, occurred_at DESC);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
```

Migration runner: a simple `runMigrations(db, fs.FS)` that scans `migrations/*.sql` in order, tracks `schema_migrations.version`, applies any not yet applied.

## Configuration

### Default `portico.yaml` (dev)

```yaml
server:
  bind: 127.0.0.1:8080
  shutdown_grace: 10s

# auth omitted => dev mode

storage:
  driver: sqlite
  dsn: file:./portico.db?cache=shared

logging:
  level: info
  format: json

skills:
  sources:
    - type: local
      path: ./skills

# tenants omitted => synthetic dev tenant
```

### Production `portico.yaml`

```yaml
server:
  bind: 0.0.0.0:8080
  shutdown_grace: 30s

auth:
  jwt:
    issuer: https://auth.example.com/
    audiences: [portico]
    jwks_url: https://auth.example.com/.well-known/jwks.json
    tenant_claim: tenant
    scope_claim: scope
    clock_skew: 60s

storage:
  driver: sqlite
  dsn: file:/var/lib/portico/portico.db?cache=shared

logging:
  level: info
  format: json

tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    entitlements:
      skills: ["*"]
      max_sessions: 200
  - id: beta
    display_name: Beta Industries
    plan: pro
    entitlements:
      skills: ["github.*", "postgres.*"]
      max_sessions: 50

skills:
  sources:
    - type: local
      path: /etc/portico/skills
```

## External APIs (Phase 0 endpoints)

```
GET  /healthz            → 200 {"status":"ok"}
GET  /readyz             → 200 {"status":"ready","version":"v0.0.0"}
GET  /v1/audit/events    → 200 {"events":[],"next_cursor":""}   (stub; real in Phase 5)
GET  /v1/admin/tenants   → 200 [{...}]                          (admin-only)
GET  /v1/admin/tenants/{id} → 200 {...}                          (admin-only)
POST /v1/admin/tenants   → 201 {...}                            (admin-only; runtime tenant create)

# Console
GET  /                   → home page (Phase 0: html/template placeholder; Phase 2+: SvelteKit SPA shell)
GET  /servers            → placeholder list (empty in Phase 0)
GET  /skills             → placeholder list
GET  /sessions           → placeholder list
GET  /static/*           → embedded assets
```

`POST /v1/admin/tenants` body shape:
```json
{"id":"foo","display_name":"Foo","plan":"pro","entitlements":{"skills":["*"],"max_sessions":50}}
```

Idempotent on `id`.

## Implementation walkthrough

### Step 1: Scaffold

```bash
cd ~/Repos/Portico_gateway
go mod init github.com/hurtener/Portico_gateway
mkdir -p cmd/portico internal/{config,auth/{jwt,tenant,scope},storage/{ifaces,sqlite/migrations},server/{api,ui},telemetry} web/console/{static,templates} testdata .github/workflows
```

Add baseline dependencies to `go.mod`:
- `github.com/go-chi/chi/v5`
- `github.com/go-chi/chi/v5/middleware`
- `github.com/golang-jwt/jwt/v5`
- `modernc.org/sqlite`
- `gopkg.in/yaml.v3`
- `github.com/oklog/ulid/v2`
- `github.com/fsnotify/fsnotify`
- (Phase 0 transitional only) stdlib `html/template`. The canonical V1 frontend stack is SvelteKit + `@sveltejs/adapter-static`; the Phase 2 plan owns the migration.
- `github.com/stretchr/testify`

### Step 2: Telemetry

`internal/telemetry/logger.go`:
- Build a `*slog.Logger` from `LoggingConfig`.
- JSON handler by default.
- Add a request-scoped child logger that auto-includes `tenant_id`, `request_id`, `trace_id` (when present).

### Step 3: Config

`internal/config/loader.go`:
- `Load(path)` reads + parses YAML, applies defaults, then `Validate`.
- `Validate`:
  - `Server.Bind` non-empty.
  - If `Auth` is nil, must be dev mode (Bind starts with `127.0.0.1` or `localhost`); else err.
  - Tenant IDs are unique, non-empty, match `^[a-z0-9][a-z0-9_-]{0,63}$`.
  - `Storage.Driver` in {`sqlite`}.
  - `JWT.TenantClaim` defaults to `"tenant"`; `JWT.ScopeClaim` defaults to `"scope"`; `JWT.ClockSkew` defaults to 60s.
- `IsDevMode()` per defined logic.

`internal/config/watcher.go`:
- fsnotify-based; debounce 200ms.
- On change: parse + validate; if valid, call `ChangeHandler(old, new)`; if invalid, log error and keep current.
- ChangeHandler may return error to indicate partial apply.

### Step 4: Storage

`internal/storage/sqlite/sqlite.go`:
- `Open(dsn string) (*DB, error)` opens connection, runs migrations.
- `DB` wraps `*sql.DB` and exposes the per-table store implementations.
- Use `embed.FS` to embed `migrations/*.sql`.

`internal/storage/sqlite/migrations.go`:
- `Run(db *sql.DB, fs embed.FS) error` applies migrations in order.

`internal/storage/sqlite/tenant_store.go`:
- Implements `ifaces.TenantStore`.
- `Upsert` does `INSERT ON CONFLICT ... DO UPDATE`.

`internal/storage/sqlite/audit_store.go`:
- Implements `ifaces.AuditStore`. `Query` returns empty slice in Phase 0 if no events.

### Step 5: Auth

`internal/auth/jwt/validator.go`:
- Parse JWT using `golang-jwt/jwt/v5`.
- Algorithm allowlist enforced via `jwt.WithValidMethods([...])`.
- Verify signature with key from JWKS.
- Verify `iss`, `aud`, `exp`, `nbf` (with skew).
- Extract claims into `Claims`.

`internal/auth/jwt/jwks.go`:
- `KeySet` interface with `LookupKey(kid string, alg string) (any, error)`.
- `StaticJWKS(path string)` reads JSON file once.
- `RemoteJWKS(url string)` fetches every 10 min; uses `Cache-Control` max-age if present.
- Concurrency-safe with `sync.RWMutex`.

`internal/auth/tenant/middleware.go`:
- See spec above.
- On dev-mode synthesis, also ensure the dev tenant exists in `TenantStore`. If missing, upsert with plan `enterprise` and display_name `"Development Tenant"`.

### Step 6: HTTP server

`internal/server/api/router.go`:
- `func NewRouter(deps Deps) http.Handler`.
- `Deps` carries logger, tenant store, audit store, config, validator, dev mode, dev tenant.
- Middleware order: requestID → recover → slog → tenant.Middleware → CORS (open in dev, configurable in prod).
- Health endpoints attached before tenant middleware.
- Routes:
  - `/v1/audit/events` — list (tenant-scoped).
  - `/v1/admin/tenants` — admin scope required.
  - All others not in this phase — Phase 1+ extends.
- 404 returns JSON `{"error":"not_found","path":"<path>"}`.
- 405 returns JSON `{"error":"method_not_allowed","method":"<method>","path":"<path>"}`.

### Step 7: Console UI

`internal/server/ui/handlers.go`:
- Phase 0 ships transitional handlers using stdlib `html/template`. They render the placeholder pages and serve the static assets via `embed.FS`. The whole `internal/server/ui/` package is rewritten in Phase 2 to embed `web/console/build/` (the SvelteKit static-adapter output) and serve a single SPA shell with index-fallback for client-side routes.
- `/` shows tenant ID, version, build date, and navigation to /servers, /skills, /sessions.
- Other pages render an empty-state message ("No servers yet — Phase 2 will populate this").
- `/static/*` served from `embed.FS` (Phase 0 transitional location; in Phase 2 this becomes whatever path the SvelteKit build emits).

### Step 8: CLI

`cmd/portico/main.go`:
- Parse subcommand argv[1].
- Dispatch to `runServe`, `runDev`, `runValidate`, `runVersion`.

`cmd/portico/cmd_serve.go`:
- Flags: `--config` (required), `--dev` (override to dev mode even with config bind != 127.0.0.1).
- `Load` config; `Validate`; open DB; build deps; start watcher; start HTTP server; on SIGINT/SIGTERM, graceful shutdown with `Server.ShutdownGrace`.

`cmd/portico/cmd_dev.go`:
- No `--config` required. Synthesizes config:
  ```go
  Config{
    Server: ServerConfig{Bind: "127.0.0.1:8080", ShutdownGrace: 5*time.Second},
    Auth:   nil,
    Storage: StorageConfig{Driver: "sqlite", DSN: "file:./portico.db?cache=shared"},
    Tenants: nil, // dev tenant synthesized
    Skills:  SkillsConfig{Sources: []SkillSourceConfig{{Type:"local", Path:"./skills"}}},
    Logging: LoggingConfig{Level: "debug", Format: "text"},
  }
  ```
- Honor `PORTICO_DEV_TENANT` env var to override tenant ID.

`cmd/portico/cmd_validate.go`:
- Flags: `--config` required.
- Load + Validate; print structured error pointing at failing field; exit 1 on failure, 0 on success.

`cmd/portico/cmd_version.go`:
- Use `runtime/debug.ReadBuildInfo()` to print version + commit + go version.

### Step 9: Makefile

```make
GO ?= go
BIN := bin/portico
LDFLAGS := -s -w
TAGS := sqlite_omit_load_extension

.PHONY: build test lint vet clean docker

build:
	CGO_ENABLED=0 $(GO) build -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/portico

test:
	$(GO) test -race -coverprofile=coverage.out ./...

vet:
	$(GO) vet ./...

lint:
	@which golangci-lint > /dev/null || (echo "install golangci-lint" && exit 1)
	golangci-lint run ./...

clean:
	rm -rf bin coverage.out

docker:
	docker build -t portico/portico:dev .
```

### Step 10: Dockerfile

Multi-stage, distroless final image:

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/portico ./cmd/portico

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/portico /portico
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/portico", "serve", "--config", "/etc/portico.yaml"]
```

### Step 11: CI

`.github/workflows/ci.yml`: run `make vet test build` on push/PR. Cache `~/go/pkg/mod`.

## Test plan

Each test file lives next to the package being tested. Integration tests in `test/integration/`.

### Unit tests

- `internal/config/config_test.go`
  - `TestLoad_ValidConfig` — parse fixture `testdata/portico.yaml`, expect 2 tenants.
  - `TestLoad_BadYAML` — parse fixture `testdata/portico-bad.yaml`, expect error mentioning the bad key.
  - `TestValidate_DuplicateTenantID` — config with two tenants of same ID, expect error.
  - `TestValidate_BadTenantID` — id with whitespace, expect error.
  - `TestValidate_AuthRequiredOutsideDev` — bind `0.0.0.0:8080` and auth nil, expect error.
  - `TestIsDevMode_*` — Bind `127.0.0.1:8080`, auth nil → true; Bind `0.0.0.0`, auth nil → false (and Validate fails); Bind `127.0.0.1`, auth set → false.

- `internal/auth/jwt/validator_test.go`
  - `TestValidate_ValidRS256` — sign a token with test key, validate, assert claims.
  - `TestValidate_ExpiredToken` — expect error wrapping `jwt.ErrTokenExpired`.
  - `TestValidate_BadSignature` — expect error.
  - `TestValidate_RejectsHS256` — sign with HS256, expect rejection (algorithm not allowed).
  - `TestValidate_BadIssuer` — expect error.
  - `TestValidate_MissingTenantClaim` — expect error.
  - `TestValidate_ClockSkew` — token expired by 30s with 60s skew, accepted.

- `internal/auth/tenant/middleware_test.go`
  - `TestMiddleware_DevModeBypass` — request with no header, dev mode, expect 200 and dev tenant injected.
  - `TestMiddleware_DevModeOverrideEnv` — `PORTICO_DEV_TENANT=foo`, expect injected tenant `foo`.
  - `TestMiddleware_ProductionRequiresJWT` — no header in prod mode, expect 401.
  - `TestMiddleware_ValidJWT` — valid token, expect identity injected.
  - `TestMiddleware_UnknownTenant` — JWT references tenant not in store, expect 401 unknown_tenant.
  - `TestMiddleware_HealthzAlwaysAllowed` — `/healthz` no header in prod, expect 200.
  - `TestMiddleware_StaticAssetsAllowed` — `/static/foo.js`, no header in prod, expect 200.

- `internal/storage/sqlite/sqlite_test.go`
  - `TestMigrations_FreshDB` — open empty DB, run migrations, expect all tables present.
  - `TestMigrations_Idempotent` — run migrations twice, no errors, no duplicate inserts.
  - `TestTenantStore_Upsert` — upsert, get, list.
  - `TestAuditStore_AppendQuery` — append three events for tenant A and one for tenant B; query A, expect three; cross-tenant isolation.
  - `TestAuditStore_QueryCursor` — append 25 events, query with limit=10, follow cursor, expect three pages of 10/10/5.

### Integration tests

- `test/integration/server_test.go`
  - `TestServerHealthz_DevMode` — boot `dev`, GET `/healthz`, expect 200.
  - `TestServerAudit_TenantScoping` — boot `serve` with two tenants and signed test JWTs; verify acme JWT cannot see beta events; verify admin scope can list both.
  - `TestServerHotReload_AddTenant` — boot, write a new tenant into the config file, wait for fsnotify debounce, send a JWT for the new tenant, expect 200.
  - `TestServerGracefulShutdown` — start, send SIGTERM, expect process exits within `shutdown_grace`, no in-flight 5xx.

### Test fixtures

`testdata/portico.yaml`:
```yaml
server:
  bind: 127.0.0.1:0  # ephemeral port for tests
auth:
  jwt:
    issuer: https://test.local/
    audiences: [portico]
    static_jwks: testdata/jwks-test.json
    tenant_claim: tenant
storage:
  driver: sqlite
  dsn: ":memory:"
tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    entitlements: { skills: ["*"], max_sessions: 100 }
  - id: beta
    display_name: Beta Industries
    plan: pro
    entitlements: { skills: ["github.*"], max_sessions: 10 }
logging:
  level: error
  format: json
skills:
  sources: []
```

`testdata/jwks-test.json`: a single RSA public key generated for tests; the corresponding private key lives in `internal/auth/jwt/testdata/test_priv.pem`.

`testdata/portico-bad.yaml`: missing `server.bind`, expecting validate error.

## Out of scope (explicitly NOT in Phase 0)

- Any MCP protocol handling (Phase 1).
- Server registry or process supervisor (Phase 2).
- Resources/prompts proxying (Phase 3).
- Skill manifest parsing or validation (Phase 4) — only the `skills` config block stub is parsed.
- Credential vault (Phase 5).
- Catalog snapshots (Phase 6).
- OpenTelemetry traces — slog logging only in Phase 0; OTel comes in Phase 6.
- Postgres support.
- Any fancy admin UI — placeholder pages only.

## Common pitfalls

- **HS\* algorithms must be rejected** in JWT validation. A subtle source of bugs: JWKS-served HS keys can let attackers forge tokens. Allowlist only asymmetric algs.
- **`per-table` tenant filtering must be enforced at the query layer**, not at handler level. Make `tenant_id` a required parameter on every storage method that touches tenant-scoped tables.
- **Dev-mode detection** should never depend on a flag alone — bind address is the safety. If someone runs `portico dev` and somehow rebinds to 0.0.0.0, the safety check should refuse.
- **`fsnotify` on macOS** can fire multiple events per file write; debounce 200ms minimum.
- **SQLite `WAL` mode** requires the directory to be writable. Validate at startup; emit a clear error if not.
- **`embed.FS` paths use forward slashes** even on Windows; don't `filepath.Join` them.
- **`golang-jwt/jwt/v5`** requires explicit `WithValidMethods`; without it, all algorithms are allowed.

## Done definition

Phase 0 is done when:
1. All acceptance criteria pass.
2. All listed tests pass with race detector enabled.
3. Coverage targets hit.
4. `make build` produces a static binary < 30 MB on Linux amd64.
5. Code passes `golangci-lint run` with the project's lint config.
6. README has a "Quickstart" section with `make build && ./bin/portico dev`.
7. A clean checkout + `make` succeeds end-to-end.

## Hand-off to Phase 1

Phase 1 inherits:
- Working `cmd/portico` binary with subcommands.
- HTTP server with tenant-aware middleware.
- SQLite store with all V1 tables (mostly empty).
- Config loader with hot reload.
- Console UI shell.
- Logging, test infrastructure, CI.

Phase 1's first job: add northbound MCP transport handlers under `internal/server/mcpgw/` and southbound MCP client adapters under `internal/mcp/southbound/{stdio,http}/`. The router already exists; Phase 1 mounts new routes without touching middleware.
