# Deployment & configuration

This guide covers how to build, configure, and run Portico in a production environment. It walks through every top-level section of `portico.yaml`, explains which fields are hot-reloadable versus restart-required, and documents the environment variables the process expects.

For a quick hands-on start without authentication or a config file, see [Dev mode](/getting-started/dev-mode).

---

## Building the binary

Portico ships as a single CGo-free static binary. The canonical build command is:

```bash
make build
```

Under the hood this runs:

```bash
CGO_ENABLED=0 go build \
  -tags 'sqlite_omit_load_extension' \
  -ldflags '-s -w' \
  -o bin/portico \
  ./cmd/portico
```

The resulting `bin/portico` binary has no external dependencies — no system libraries, no shared objects, no runtime interpreter. Copy it to any Linux or macOS host and run it directly.

Cross-compile for a different platform with standard Go toolchain flags:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -tags 'sqlite_omit_load_extension' \
  -ldflags '-s -w' \
  -o bin/portico-linux-amd64 \
  ./cmd/portico
```

---

## Running in production

```bash
portico serve --config /etc/portico/portico.yaml
```

The `serve` subcommand:
1. Reads and validates `portico.yaml` (see [Validating config](#validating-config) below).
2. Opens (and migrates) the SQLite database at the path in `storage.dsn`.
3. Seeds tenants and Agent Profiles from the config file into the database (idempotent on restart).
4. Starts a config file watcher for hot-reload of select fields.
5. Starts the HTTP listener on `server.bind`.
6. Handles `SIGINT` / `SIGTERM` with a graceful shutdown of up to `server.shutdown_grace`.

`--config` is required; there is no default path.

---

## portico.yaml reference

The file is parsed as YAML. Unknown keys are tolerated for forward-compatibility — upgrading the binary does not require updating every config file immediately.

### `server`

Controls the HTTP listener. The same listener serves REST, MCP, A2A, and the embedded Console SPA.

```yaml
server:
  bind: "0.0.0.0:8080"        # required; host:port to listen on
  shutdown_grace: 10s          # graceful shutdown budget; default 10s
  allowed_origins:             # browser-side Origin allowlist (optional)
    - "https://console.example.com"
```

| Key | Type | Required | Default | Notes |
|-----|------|----------|---------|-------|
| `bind` | string | yes | — | `host:port`; bind `127.0.0.1` to disable external access |
| `shutdown_grace` | duration | no | `10s` | SIGTERM → graceful → SIGKILL window |
| `allowed_origins` | list | no | `[]` | MCP Origin guard; `*` allows any browser origin; programmatic clients without an Origin header are always allowed |

::: warning Auth is required for non-localhost binds
If `auth` is absent and `server.bind` is not a loopback address, the process refuses to start. This prevents accidental open deployments. See [dev mode](/getting-started/dev-mode) for localhost-only unauthenticated use.
:::

### `auth`

Configures the JWT validator. Portico only accepts asymmetric algorithms (RS256/RS384/RS512, ES256/ES384/ES512). HS\* and `none` are rejected at startup.

```yaml
auth:
  jwt:
    issuer: "https://auth.example.com/"        # required; must match the `iss` claim
    audiences: ["portico"]                      # required; checked against the `aud` claim
    jwks_url: "https://auth.example.com/.well-known/jwks.json"
    # OR: static_jwks: "/etc/portico/jwks.json"  (mutually exclusive with jwks_url)
    tenant_claim: "tenant"                      # default "tenant"
    scope_claim: "scope"                        # default "scope"
    required_scope: "portico:access"            # optional global scope gate
    clock_skew: 60s                             # default 60s
```

| Key | Required | Default | Notes |
|-----|----------|---------|-------|
| `issuer` | yes | — | Matched against the `iss` JWT claim |
| `audiences` | yes | — | Any one audience in the list satisfies the check |
| `jwks_url` | one of | — | Fetched and cached at startup; refreshed on key-not-found |
| `static_jwks` | one of | — | Path to a local JWKS file; mutually exclusive with `jwks_url` |
| `tenant_claim` | no | `"tenant"` | JWT claim whose value is the tenant ID |
| `scope_claim` | no | `"scope"` | JWT claim containing the scope string |
| `required_scope` | no | — | If set, every request must carry this scope |
| `clock_skew` | no | `60s` | Tolerance applied to `exp` / `nbf` |

See [Authentication](/concepts/authentication) for the full token-flow and multi-tenant scope design.

### `storage`

Portico V1 uses a single embedded SQLite database. Migrations run automatically on startup.

```yaml
storage:
  driver: sqlite                                      # only driver in V1
  dsn: "file:/var/lib/portico/portico.db?cache=shared"
```

`ensureDataDir` is called at boot: the directory portion of the path is created with permissions `0750` if it does not exist, so there is no need to pre-create it.

For in-memory operation (testing or ephemeral scenarios):

```yaml
storage:
  driver: sqlite
  dsn: ":memory:"
```

### `tenants`

Each tenant gets an isolated namespace: separate registry entries, skill enablement, audit events, vault entries, and budget trees. See [Multi-tenancy](/concepts/multi-tenancy) for the isolation model.

```yaml
tenants:
  - id: acme                       # lowercase alphanum + _ -; 1–64 chars
    display_name: "Acme Corp"
    plan: enterprise                # free | pro | enterprise (operator-defined)
    entitlements:
      skills: ["*"]                 # glob patterns; "*" = all skills
      max_sessions: 200
  - id: beta-org
    display_name: "Beta Organisation"
    plan: pro
    entitlements:
      skills: ["customer-support.*", "github.*"]
      max_sessions: 20
```

Tenant IDs must match `^[a-z0-9][a-z0-9_-]{0,63}$`. Duplicate IDs are rejected at parse time. Tenants declared here are upserted into the database at every boot, so adding a tenant to the config and restarting is sufficient to provision it.

### `skills`

Declares the Skill Pack sources the runtime should load. See [Skill Packs](/concepts/skill-packs) for the manifest format and build guide.

```yaml
skills:
  enablement_default: "opt-in"   # opt-in (default) | auto
  sources:
    - type: local
      path: "./skill-packs"
    - type: local
      path: "/opt/portico/skills/company-pack"
```

`enablement_default: auto` enables every skill in every entitled tenant automatically at startup. `opt-in` requires an explicit enablement call per tenant through the API or Console.

### `logging`

```yaml
logging:
  level: info     # debug | info | warn | error; default info
  format: json    # json (default) | text
```

Use `format: text` for human-readable output in development or when logs feed a terminal directly. JSON is the right choice for any structured log aggregator.

### `telemetry`

Wires OpenTelemetry tracing and the drift detector.

```yaml
telemetry:
  enabled: true
  service_name: "portico-prod"
  exporter: otlp_grpc             # otlp_grpc | otlp_http | stdout | none
  otlp_endpoint: "http://otel-collector:4317"
  otlp_headers:
    "x-honeycomb-team": "YOUR_API_KEY"
  sample_rate: 0.1                # 10% sampling; 1.0 = 100%
  resource_attrs:
    deployment.environment: production
    host.name: portico-prod-01
  drift_interval: 60s             # how often the drift detector polls; default 60s
```

See [Observability](/concepts/observability) for the full span model and drift detection behaviour.

### `servers`

Declares downstream MCP servers to register at boot time. These are upserted into every configured tenant's registry. Per-tenant overrides and dynamic registration via the REST API are also supported; the config-declared servers are always present even after a restart.

Server IDs must match `^[a-z0-9][a-z0-9_-]{0,31}$`.

**stdio transport:**

```yaml
servers:
  - id: filesystem
    display_name: "Filesystem MCP"
    transport: stdio
    runtime_mode: per_tenant        # per_tenant | per_user | per_session
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
      env:
        - NODE_ENV=production
      cwd: /opt/portico
    start_timeout: 30s
```

**HTTP transport:**

```yaml
servers:
  - id: github
    display_name: "GitHub MCP"
    transport: http
    http:
      url: "https://mcp.github.com/"
      timeout: 30s
    auth:
      strategy: header
      secret_ref: github_token      # resolved from the vault at call time
```

**OAuth token exchange (RFC 8693):**

```yaml
servers:
  - id: internal-api
    transport: http
    http:
      url: "https://api.internal/mcp"
    auth:
      strategy: oauth_exchange
      exchange:
        token_url: "https://idp.internal/token"
        client_id: "portico-gw"
        client_secret_ref: idp_client_secret
        audience: "https://api.internal"
        scope: "mcp.call"
```

See [Register an MCP server](/guides/register-mcp-server) for the full lifecycle and [Credentials vault](/concepts/credentials-vault) for vault-backed secret references.

### `code_mode`

The Code Mode block tunes the `executeToolCode` meta-tool. Omit the block entirely for the open default (no restrictions beyond standard tenant isolation).

```yaml
code_mode:
  disabled: false                    # kill switch: deny every executeToolCode
  max_execution_bytes: 65536         # reject snippets larger than N bytes (0 = no limit)
  max_tool_calls_inside: 20          # ceiling on in-sandbox tool calls per run (0 = runtime default)
  allowed_binding_levels: []         # restrict to ["server"] or ["tool"]; empty = any
  require_approval_on_execute: false # gate every execution behind the approval flow
  deny_unsafe_starlark: false        # record static-gate rejections as audited policy denials
```

See [Code Mode](/concepts/code-mode) for the sandbox security model.

### `agent_profiles`

Declares Agent Profiles for cold-start seeding. Profiles are the single source of truth for what a consumer can see and call. Seeding is idempotent: an existing profile is matched by `(tenant, name)` and updated in place, so restarting never duplicates it.

```yaml
agent_profiles:
  - name: support-eu
    tenant: acme                          # required when multiple tenants are configured
    description: "EU customer support agent"
    allowed_mcp_servers: [zendesk, intercom-eu]
    allowed_tools:
      - zendesk.search_tickets
      - zendesk.get_ticket
    allowed_skills: [support-triage]
    allowed_model_aliases: [fast-summary]
    scopes: [mcp:call, llm:invoke]
    bindings: [support-agent-eu@acme]     # JWT subjects bound at boot
    enabled: true
```

See [Agent Profiles](/concepts/agent-profiles) for the full entitlement model and virtual key binding.

### `cache`

Configures the semantic cache layer in front of the LLM gateway. Absent or `driver: ""` means no caching (the default, equivalent to V1 behaviour).

```yaml
cache:
  driver: redis                   # none | inmem | redis | weaviate | qdrant
  scope: tenant                   # tenant (default) | customer | team | vk
  ttl: 5m                         # entry lifetime; empty = driver default
  threshold: 0.92                 # semantic-similarity floor for vector drivers (0–1)
  options:                        # driver-specific block
    addr: "redis:6379"
    password: ""
    db: 0
```

Cache entries are always scoped by `tenant_id` first regardless of the `scope` setting — cross-tenant cache sharing is not possible by construction. See [Semantic cache](/concepts/semantic-cache) for the full model.

---

## Realistic portico.yaml

The following is a representative production configuration. Replace placeholder values with your real endpoints and tenant identifiers.

```yaml
server:
  bind: "0.0.0.0:8080"
  shutdown_grace: 15s
  allowed_origins:
    - "https://console.corp.example.com"

auth:
  jwt:
    issuer: "https://auth.corp.example.com/"
    audiences: ["portico"]
    jwks_url: "https://auth.corp.example.com/.well-known/jwks.json"
    tenant_claim: tenant
    scope_claim: scope
    clock_skew: 60s

storage:
  driver: sqlite
  dsn: "file:/var/lib/portico/portico.db?cache=shared"

tenants:
  - id: corp
    display_name: "Corp Internal"
    plan: enterprise
    entitlements:
      skills: ["*"]
      max_sessions: 500

skills:
  enablement_default: opt-in
  sources:
    - type: local
      path: /opt/portico/skill-packs

logging:
  level: info
  format: json

telemetry:
  enabled: true
  service_name: portico-prod
  exporter: otlp_http
  otlp_endpoint: "http://otel-collector.infra:4318"
  sample_rate: 0.05
  drift_interval: 60s

servers:
  - id: github
    display_name: "GitHub MCP"
    transport: http
    http:
      url: "https://mcp.github.com/"
      timeout: 30s
    auth:
      strategy: header
      secret_ref: github_token

  - id: filesystem
    display_name: "Internal Filesystem"
    transport: stdio
    runtime_mode: per_tenant
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data/shared"]
    start_timeout: 30s

code_mode:
  disabled: false
  max_execution_bytes: 65536
  max_tool_calls_inside: 20
  require_approval_on_execute: true

agent_profiles:
  - name: ops-assistant
    tenant: corp
    description: "Internal ops automation agent"
    allowed_mcp_servers: [github, filesystem]
    allowed_tools:
      - github.create_issue
      - github.search_repositories
      - filesystem.read_file
      - filesystem.list_directory
    allowed_model_aliases: [default]
    scopes: [mcp:call, llm:invoke]
    bindings: [ops-bot@corp]

cache:
  driver: inmem
  scope: tenant
  ttl: 10m
  threshold: 0.92
```

---

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `PORTICO_VAULT_KEY` | for vault | Base64-encoded 32-byte AES-256 master key. When set, Portico loads the file vault from `<data_dir>/vault.yaml` and secret references in server configs resolve against it. When absent, vault features are disabled and any `secret_ref` in server auth blocks will surface as start failures. |
| `PORTICO_DEV_TENANT` | no | Overrides the synthetic tenant ID used in dev mode (default `dev`). Has no effect in production mode (`serve` with `auth` configured). |

**Generating a vault key:**

```bash
# Generate a random 32-byte key and base64-encode it (44 characters).
# Inject the value through your platform's secret store — do not commit it.
export PORTICO_VAULT_KEY="$(openssl rand -base64 32)"
```

**Storing secrets in the vault:**

```bash
# Put a secret (prompts for value on stdin if --from-stdin)
portico vault put --tenant corp --name github_token --value "ghp_..."

# List secrets for a tenant
portico vault list --tenant corp

# Rotate the master key (re-encrypts all entries in place)
portico vault rotate-key --new-key "$(openssl rand -base64 32)"
```

After rotating the key, update `PORTICO_VAULT_KEY` before the next process restart.

---

## Hot-reloadable vs restart-required fields

Portico watches `portico.yaml` for changes using filesystem notifications (200 ms debounce). When the file is written, the new config is parsed and validated; if valid, the change is applied without restarting the process.

::: info What actually reloads
The config watcher calls `seedRegistryFromConfig` on each successful reload. This means **server declarations** (`servers.*`) are hot-reloadable — you can add, remove, or edit a server entry and the change propagates to the registry within seconds. The affected downstream connections are drained and re-established on next use.
:::

All other top-level sections require a process restart to take effect:

| Section | Hot-reloadable |
|---------|---------------|
| `servers` | **Yes** — new/removed/edited specs propagate immediately |
| `server.bind` | No — changing the listening address requires restart |
| `server.shutdown_grace` | No |
| `server.allowed_origins` | No |
| `auth.jwt.*` | No — JWT validator is constructed once at boot |
| `storage.*` | No — the database connection is opened once at boot |
| `tenants` | No — tenant seeding runs only at boot |
| `skills.*` | No |
| `logging.*` | No |
| `telemetry.*` | No |
| `code_mode.*` | No |
| `agent_profiles` | No — seeding runs only at boot |
| `cache.*` | No |

For a zero-downtime rollout of sections that require restart, run multiple instances behind a load balancer and perform a rolling restart — the binary boots quickly and the SQLite WAL mode supports concurrent readers during the transition.

---

## Validating config

Before deploying or restarting, validate the config file:

```bash
portico validate --config /etc/portico/portico.yaml
```

On success the command prints a summary and exits 0:

```
config OK: bind=0.0.0.0:8080 tenants=1 storage=sqlite dev_mode=false
```

On failure it prints the first offending field and exits 1, for example:

```
portico: config: auth.jwt: either jwks_url or static_jwks is required
```

Validating Skill Pack manifests is a separate command:

```bash
portico validate-skills /opt/portico/skill-packs/...
```

---

## Docker / container deployment

Portico is designed as a distroless multi-stage container image: a builder stage compiles the CGo-free binary, then a minimal distroless runtime stage copies only the binary and the data directory.

Build the image with:

```bash
make docker
# equivalent to: docker build -t portico/portico:dev .
```

A representative production `Dockerfile` follows the standard Go distroless pattern:

```dockerfile
# Stage 1: build
FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
      -tags sqlite_omit_load_extension \
      -ldflags '-s -w' \
      -o /bin/portico ./cmd/portico

# Stage 2: minimal runtime
FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/portico /portico
ENTRYPOINT ["/portico"]
CMD ["serve", "--config", "/etc/portico/portico.yaml"]
```

Mount your config file and data directory as volumes:

```bash
docker run \
  -v /etc/portico:/etc/portico:ro \
  -v /var/lib/portico:/var/lib/portico \
  -e PORTICO_VAULT_KEY="$(cat /run/secrets/vault_key)" \
  -p 8080:8080 \
  portico/portico:dev
```

::: tip Data directory permissions
The process creates the directory named in `storage.dsn` with permissions `0750` if it is absent. In a container, ensure the volume mount point is writable by the user the process runs as, or pre-create it.
:::

For Kubernetes deployments, the recommended pattern is:

- `portico.yaml` in a `ConfigMap`
- `PORTICO_VAULT_KEY` in a `Secret`, projected as an environment variable
- A `PersistentVolumeClaim` for the SQLite data directory
- A `Deployment` with a single replica (SQLite WAL supports many readers but one writer)

---

## Related

- [Configuration reference](/reference/configuration) — complete schema documentation with type annotations
- [Authentication](/concepts/authentication) — JWT validation, tenant claim extraction, and scope enforcement
- [Manage providers](/guides/manage-providers) — register LLM providers and model aliases after the process is running
- [Credentials vault](/concepts/credentials-vault) — AES-256-GCM vault, secret references, and key rotation
- [Agent Profiles](/concepts/agent-profiles) — consumer entitlement bindings and virtual key association
- [Virtual Keys](/concepts/virtual-keys) — scoped, HMAC-verified API keys for agent access
- [Observability](/concepts/observability) — OpenTelemetry tracing, span retention, and drift detection
