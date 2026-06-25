# Configuration

Portico is configured through a single YAML file — conventionally named `portico.yaml` — passed to the binary via `--config`. This page is the authoritative reference for every field, its type, its default, and whether it can be updated without a process restart.

::: tip Dev mode
If `auth` is omitted **and** `server.bind` resolves to a loopback address (`127.0.0.1`, `::1`, or `localhost`), Portico enters dev mode: JWT validation is disabled and a synthetic `dev` tenant is created automatically. Dev mode is safe only for local development — attempting it with a non-loopback bind is a startup error.
:::

## Loading and validation

```bash
./bin/portico serve --config /etc/portico/portico.yaml
```

At startup, `config.Load` reads the file, decodes YAML (unknown keys are tolerated for forward-compatibility), and calls `Validate`, which fills in defaults and rejects any inconsistent combination of fields. A `FieldError` is returned on failure; the message includes a JSON-pointer-style path such as `config: auth.jwt.issuer: is required`.

## Hot reload

When running under `serve`, Portico watches the config file for changes using an `fsnotify` watcher. On a detected write, create, or rename event, the file is re-parsed and validated. Changes to the `servers` list are applied immediately — the registry is re-seeded and affected downstream processes are restarted. All other top-level sections (authentication, storage, telemetry, code mode, and the HTTP listener) are wired at boot and **require a process restart** to take effect. A failed reload logs a warning and keeps the previous configuration; no traffic is interrupted.

---

## Top-level structure

```yaml
server: { ... }
auth: { ... }           # omit for dev mode
storage: { ... }
tenants: [ ... ]
skills: { ... }
logging: { ... }
telemetry: { ... }      # optional
servers: [ ... ]        # optional
code_mode: { ... }      # optional
agent_profiles: [ ... ] # optional
cache: { ... }          # optional
```

---

## `server`

Controls the HTTP listener shared by the REST API, MCP northbound, A2A northbound, and the embedded Console.

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `bind` | string | — | No | `host:port` the server listens on. Required. Example: `0.0.0.0:8080`. |
| `shutdown_grace` | duration | `10s` | No | How long `SIGTERM` waits for in-flight requests to drain before forcing close. |
| `allowed_origins` | []string | `[]` | No | Origin header allowlist for the MCP Streamable HTTP endpoint. An empty list permits all programmatic clients (no `Origin` header). Browser clients must be listed explicitly. `"*"` allows any origin. Dev mode automatically permits localhost. |

**Example:**

```yaml
server:
  bind: 0.0.0.0:8080
  shutdown_grace: 30s
  allowed_origins:
    - https://console.example.com
```

---

## `auth`

Omit entirely to use dev mode (loopback bind required). Currently supports JWT bearer token validation only. The JWT validator accepts asymmetric algorithms only: RS256, RS384, RS512, ES256, ES384, ES512. HS\* and `none` are rejected at startup.

### `auth.jwt`

| Field | Type | Default | Required | Description |
|---|---|---|---|---|
| `issuer` | string | — | Yes | Expected `iss` claim value. |
| `audiences` | []string | — | No | Accepted `aud` values. At least one audience is recommended for production. |
| `jwks_url` | string | — | One of these | URL of a remote JWKS endpoint. Mutually exclusive with `static_jwks`. |
| `static_jwks` | string | — | One of these | Path to a local JWKS file on disk. Mutually exclusive with `jwks_url`. |
| `tenant_claim` | string | `"tenant"` | No | JWT claim that carries the tenant identifier. |
| `scope_claim` | string | `"scope"` | No | JWT claim that carries granted scopes. |
| `required_scope` | string | `""` | No | Optional global scope that every token must contain. |
| `clock_skew` | duration | `60s` | No | Tolerance window for `iat`/`exp`/`nbf` validation. |

**Example:**

```yaml
auth:
  jwt:
    issuer: https://auth.example.com/
    audiences: [portico]
    jwks_url: https://auth.example.com/.well-known/jwks.json
    tenant_claim: tenant
    scope_claim: scope
    required_scope: portico:access
    clock_skew: 30s
```

For environments where the IdP is not reachable at startup, use `static_jwks` pointing to a pre-fetched JWKS file. See [Authentication](/concepts/authentication) for a discussion of claim mapping and scope design.

---

## `storage`

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `driver` | string | `"sqlite"` | No | Storage backend driver. `"sqlite"` is the only supported driver in V1. |
| `dsn` | string | `"file:./portico.db?cache=shared"` | No | Driver-specific data source name. For SQLite, use `"file:/var/lib/portico/portico.db?cache=shared"` in production and `":memory:"` for tests. |

The data directory is created automatically if it does not exist. WAL journal mode is enabled by the first migration; do not override it in the DSN.

```yaml
storage:
  driver: sqlite
  dsn: "file:/var/lib/portico/portico.db?cache=shared&_busy_timeout=5000"
```

---

## `tenants`

A list of tenant definitions seeded at boot. Each entry is upserted idempotently by `id`, so restarts do not create duplicates.

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Tenant identifier. Must match `^[a-z0-9][a-z0-9_-]{0,63}$`. Must be unique. |
| `display_name` | string | No | Human-readable label shown in the Console. Defaults to `id` if empty. |
| `plan` | string | No | Billing plan tier: `free`, `pro`, `enterprise`, or any operator-defined value. Defaults to `free`. |
| `credentials_ref` | string | No | Reserved for future use. |
| `entitlements.skills` | []string | No | Glob patterns of Skill Pack IDs the tenant may enable. `["*"]` allows all. |
| `entitlements.max_sessions` | int | No | Maximum concurrent MCP sessions for this tenant. `0` means unlimited. |
| `metadata` | map[string]string | No | Arbitrary key-value pairs stored with the tenant record. |

**Example:**

```yaml
tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    entitlements:
      skills: ["*"]
      max_sessions: 500
    metadata:
      region: us-east-1
  - id: startupco
    display_name: Startup Co
    plan: pro
    entitlements:
      skills: ["github.*", "linear.*"]
      max_sessions: 20
```

See [Multi-Tenancy](/concepts/multi-tenancy) for a detailed explanation of tenant isolation.

---

## `skills`

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `sources` | []SkillSourceConfig | `[]` | No | Skill Pack source declarations. Empty list disables the Skills runtime entirely. |
| `enablement_default` | string | `"opt-in"` | No | `"opt-in"`: skills must be explicitly enabled per-tenant. `"auto"`: all entitled skills are enabled at load time. |

### `skills.sources[]`

| Field | Type | Required | Description |
|---|---|---|---|
| `type` | string | Yes | Source driver type. `"local"` reads manifests from a directory on disk. Additional drivers (`git`, `http`) are available via the source registry. |
| `path` | string | Depends | Filesystem path to the skill pack directory. Required when `type: local`. |

```yaml
skills:
  enablement_default: opt-in
  sources:
    - type: local
      path: ./skills
```

See [Skill Packs](/concepts/skill-packs) and [Skill Sources](/concepts/skill-sources) for authoring and source management.

---

## `logging`

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `level` | string | `"info"` | No | Minimum log level: `debug`, `info`, `warn`, or `error`. |
| `format` | string | `"json"` | No | Log format: `json` (structured, for log aggregation) or `text` (human-readable for terminal use). |

```yaml
logging:
  level: info
  format: json
```

---

## `telemetry`

All telemetry fields are optional. Omitting the entire block leaves tracing disabled.

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `enabled` | bool | `false` | No | Enable the OpenTelemetry tracer. |
| `service_name` | string | `""` | No | `service.name` resource attribute attached to every span. |
| `exporter` | string | `""` | No | Exporter backend: `otlp_grpc`, `otlp_http`, `stdout`, or `none`. |
| `otlp_endpoint` | string | `""` | No | OTLP receiver endpoint (e.g. `http://collector:4318`). |
| `otlp_headers` | map[string]string | `{}` | No | Headers sent with every OTLP export request (e.g. authentication). |
| `sample_rate` | float64 | `0` | No | Fraction of traces to sample (0–1). `0` uses the exporter default. |
| `resource_attrs` | map[string]string | `{}` | No | Additional OTel resource attributes (e.g. `deployment.environment: production`). |
| `drift_interval` | duration string or int | `60s` | No | How often the drift detector snapshots downstream tool lists and compares against cached state. Accepts Go duration strings (`"5m"`) or an integer number of seconds. |

```yaml
telemetry:
  enabled: true
  service_name: portico-gateway
  exporter: otlp_http
  otlp_endpoint: http://otel-collector:4318
  otlp_headers:
    Authorization: Bearer ${OTEL_TOKEN}
  sample_rate: 0.1
  resource_attrs:
    deployment.environment: production
    region: us-east-1
  drift_interval: 5m
```

See [Observability](/concepts/observability) for the full telemetry architecture.

---

## `servers`

A list of MCP server registrations seeded at boot. Servers defined here are upserted into every configured tenant's registry. The `servers` list is the only top-level section that supports hot reload — editing the file triggers an immediate re-seed; affected downstream processes are drained and restarted.

### Core fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Unique server identifier. Must match `^[a-z0-9][a-z0-9_-]{0,31}$`. |
| `display_name` | string | No | Human-readable label. |
| `transport` | string | Yes | `stdio` or `http`. |
| `runtime_mode` | string | No | Process lifecycle: `per_tenant`, `per_user`, or `per_session`. Defaults to `per_tenant` when omitted. |
| `start_timeout` | duration | No | Budget for the southbound initialize handshake. |

### `servers[].stdio`

Required when `transport: stdio`.

| Field | Type | Required | Description |
|---|---|---|---|
| `command` | string | Yes | Executable path or name. Never passed through a shell — arguments must be in `args`. |
| `args` | []string | No | Argument vector. |
| `env` | []string | No | Additional environment variables in `KEY=VALUE` form. Supports <span v-pre>`{{secret:name}}`</span> interpolation when the vault is configured. |
| `cwd` | string | No | Working directory for the child process. |

### `servers[].http`

Required when `transport: http`.

| Field | Type | Required | Description |
|---|---|---|---|
| `url` | string | Yes | Base URL of the downstream MCP HTTP server. |
| `auth_header` | string | No | Verbatim `Authorization` header value. Use `secret_ref` (under `auth`) for values stored in the vault. |
| `timeout` | duration | No | Per-request HTTP timeout. |

### `servers[].auth`

Optional credential strategy applied before each southbound call.

| Field | Type | Description |
|---|---|---|
| `strategy` | string | Injection strategy: `env`, `header`, `secret_ref`, or `oauth_exchange`. |
| `default_risk_class` | string | Override the policy engine's risk classification for all tools on this server. |
| `env` | []string | Environment variable names whose values are pulled from the vault and injected into the child process environment. |
| `headers` | map[string]string | HTTP header names to inject (values resolved from vault). |
| `secret_ref` | string | Vault secret name whose value is written as the `Authorization` header. |
| `exchange` | OAuthExchangeSpec | RFC 8693 token exchange configuration. See below. |

### `servers[].auth.exchange`

| Field | Type | Description |
|---|---|---|
| `token_url` | string | Token endpoint URL. |
| `client_id` | string | OAuth client identifier. |
| `client_secret_ref` | string | Vault secret name for the client secret. |
| `audience` | string | Requested token audience. |
| `scope` | string | Requested token scope. |
| `grant_type` | string | OAuth grant type. Defaults to `urn:ietf:params:oauth:grant-type:token-exchange`. |
| `subject_token_src` | string | Source of the subject token (e.g. `caller_jwt`). |

**Example — stdio server with vault-injected credentials:**

```yaml
servers:
  - id: github-mcp
    display_name: GitHub MCP Server
    transport: stdio
    runtime_mode: per_session
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        - GITHUB_PERSONAL_ACCESS_TOKEN={{secret:github-pat}}
    auth:
      strategy: env
      default_risk_class: write

  - id: internal-api
    display_name: Internal REST API
    transport: http
    http:
      url: https://api.internal.example.com/mcp
      timeout: 15s
    auth:
      strategy: secret_ref
      secret_ref: internal-api-token
```

See [Register an MCP Server](/guides/register-mcp-server) and [OAuth Token Exchange](/concepts/oauth-token-exchange) for full walkthrough.

---

## `code_mode`

Code Mode exposes a sandboxed Starlark execution environment to AI agents via the `executeToolCode` MCP tool. This section lets operators tune or disable that surface. Omitting the block enables the permissive default — all snippet executions are permitted within normal tenant governance.

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `disabled` | bool | `false` | No | Kill switch. When `true`, every `executeToolCode` call is denied regardless of session state. |
| `max_execution_bytes` | int | `0` | No | Reject snippets larger than this byte count. `0` disables the limit. |
| `max_tool_calls_inside` | int | `0` | No | Cap on tool calls made from within a single snippet execution. Acts as a ceiling — it can lower a session's configured limit but never raise it. `0` uses the runtime default. |
| `allowed_binding_levels` | []string | `[]` | No | Restrict which binding levels may invoke Code Mode: `server` or `tool`. Empty allows any level. |
| `require_approval_on_execute` | bool | `false` | No | Route every execution through the approval flow before the snippet runs. |
| `deny_unsafe_starlark` | bool | `false` | No | Escalate static-gate rejections (unsafe built-ins detected at parse time) to audited policy denial events. |

```yaml
code_mode:
  disabled: false
  max_execution_bytes: 65536
  max_tool_calls_inside: 20
  require_approval_on_execute: true
  deny_unsafe_starlark: true
```

See [Code Mode](/concepts/code-mode) for a full explanation of the execution model and threat surface.

---

## `agent_profiles`

Agent Profiles declare named entitlement boundaries for AI consumers: which MCP servers, tools, Skill Packs, model aliases, and JWT scopes a logical agent may access. Profiles declared here are seeded idempotently at boot (matched by `(tenant, name)`; restarts update in place without creating duplicates). The REST API and Console provide full CRUD on top of these bootstrap records.

Omitting this block leaves every authenticated request using the default full-surface profile — the V1 behavior.

| Field | Type | Required | Description |
|---|---|---|---|
| `tenant` | string | Conditional | Tenant ID this profile belongs to. May be omitted when exactly one tenant is configured; required when zero or multiple tenants are declared. |
| `name` | string | Yes | Profile name. Unique within a tenant. |
| `description` | string | No | Human-readable purpose statement. |
| `allowed_mcp_servers` | []string | No | MCP server IDs this profile may invoke. Empty means all servers the tenant has access to. |
| `allowed_tools` | []string | No | Qualified tool names (`server-id.tool-name`) this profile may call. Empty means all tools on allowed servers. |
| `allowed_skills` | []string | No | Skill Pack IDs this profile may use. Empty means all enabled skills. |
| `allowed_model_aliases` | []string | No | LLM model alias identifiers this profile may route to. Empty means all models. |
| `scopes` | []string | No | JWT scope intersection applied to this profile. Narrows — never broadens — the caller's token scopes. |
| `bindings` | []string | No | JWT subject values (`sub` claim) bound to this profile at boot. The mapping is idempotent. |
| `enabled` | bool | No | Defaults to `true`. Set to `false` to provision a profile in a disabled state. |

**Example:**

```yaml
agent_profiles:
  - name: support-agent
    tenant: acme
    description: Read-only customer support agent
    allowed_mcp_servers: [zendesk, intercom]
    allowed_tools:
      - zendesk.search_tickets
      - zendesk.get_ticket
    allowed_skills: [support-triage]
    allowed_model_aliases: [fast-chat]
    scopes: [mcp:call, llm:invoke]
    bindings: [support-bot@acme]
    enabled: true
```

See [Agent Profiles](/concepts/agent-profiles) and [Create an Agent Profile](/guides/create-agent-profile).

---

## `cache`

The semantic cache sits in front of the LLM gateway and short-circuits duplicate or near-duplicate requests. Omitting the block, or setting `driver: ""` / `driver: none`, disables caching; all behavior is identical to a gateway without this section.

| Field | Type | Default | Hot-reload | Description |
|---|---|---|---|---|
| `driver` | string | `""` | No | Cache driver: `none`, `inmem`, `redis`, `weaviate`, or `qdrant`. Empty and `"none"` are equivalent. |
| `scope` | string | `"tenant"` | No | Cache key partitioning: `tenant` (shared across the whole tenant), `customer`, `team`, or `vk` (per-Virtual Key). Cross-tenant sharing is impossible by construction. |
| `ttl` | string | driver default | No | Default entry lifetime as a Go duration string (e.g. `"5m"`, `"1h"`). |
| `threshold` | float32 | `0` | No | Semantic similarity floor (0–1) for embedding-based drivers (`weaviate`, `qdrant`). A value of `0` uses the driver default. |
| `options` | map[string]any | `{}` | No | Driver-specific configuration block (see below). |

### Driver-specific options

**`inmem`** — in-process LRU cache, no persistence.

```yaml
cache:
  driver: inmem
  ttl: 10m
  options:
    max_entries: 4096
```

**`redis`** — Redis-backed cache.

```yaml
cache:
  driver: redis
  scope: tenant
  ttl: 30m
  options:
    addr: redis:6379
    password: "${REDIS_PASSWORD}"
    db: 0
```

**`weaviate`** — Weaviate vector store for semantic similarity matching.

```yaml
cache:
  driver: weaviate
  scope: tenant
  ttl: 1h
  threshold: 0.95
  options:
    endpoint: http://weaviate:8080
    class_name: PorticoCache
```

See [Semantic Cache](/concepts/semantic-cache) for the full caching architecture, eviction policy, and tenant isolation guarantees.

---

## Environment variables

These variables supplement `portico.yaml`. They are not part of the YAML schema.

| Variable | Required | Description |
|---|---|---|
| `PORTICO_VAULT_KEY` | When vault is used | AES-256 master key for the file vault, encoded as standard Base64. Must decode to exactly 32 bytes. Absent means the vault is disabled; <span v-pre>`{{secret:...}}`</span> references in stdio `env` entries will cause server start failures. Generate with `openssl rand -base64 32`. |
| `PORTICO_DEV_TENANT` | No | Overrides the synthetic tenant ID used in dev mode. Defaults to `"dev"`. Has no effect when `auth` is configured. |
| `PORTICO_PREFLIGHT_SKIP` | No | Set to `1` to skip the pre-commit preflight hook on a local machine in an emergency. CI always runs the full gate regardless. |

### Generating `PORTICO_VAULT_KEY`

```bash
# Generate and export a fresh 32-byte key
export PORTICO_VAULT_KEY="$(openssl rand -base64 32)"

# Rotate the key after storing it in a secrets manager
./bin/portico vault rotate-key --new-key "$NEW_KEY"
# Then restart with PORTICO_VAULT_KEY=$NEW_KEY
```

::: warning Never commit the key
`PORTICO_VAULT_KEY` must be provided as a runtime secret through your process manager, container environment, or secrets manager. It must never appear in `portico.yaml`, Dockerfiles, or source control.
:::

See [Credentials Vault](/concepts/credentials-vault) for vault operations and key rotation.

---

## Complete annotated example

The following `portico.yaml` demonstrates all top-level sections in a typical production deployment.

```yaml
server:
  bind: 0.0.0.0:8080
  shutdown_grace: 30s
  allowed_origins:
    - https://console.example.com

auth:
  jwt:
    issuer: https://auth.example.com/
    audiences: [portico]
    jwks_url: https://auth.example.com/.well-known/jwks.json
    tenant_claim: tenant
    scope_claim: scope
    clock_skew: 30s

storage:
  driver: sqlite
  dsn: "file:/var/lib/portico/portico.db?cache=shared&_busy_timeout=5000"

tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    entitlements:
      skills: ["*"]
      max_sessions: 500

skills:
  enablement_default: opt-in
  sources:
    - type: local
      path: /etc/portico/skills

logging:
  level: info
  format: json

telemetry:
  enabled: true
  service_name: portico-gateway
  exporter: otlp_http
  otlp_endpoint: http://otel-collector:4318
  sample_rate: 0.05
  drift_interval: 5m

servers:
  - id: github-mcp
    display_name: GitHub MCP Server
    transport: stdio
    runtime_mode: per_session
    stdio:
      command: npx
      args: ["-y", "@modelcontextprotocol/server-github"]
      env:
        - GITHUB_PERSONAL_ACCESS_TOKEN={{secret:github-pat}}
    auth:
      strategy: env

  - id: internal-search
    display_name: Internal Search API
    transport: http
    http:
      url: https://search.internal.example.com/mcp
      timeout: 10s
    auth:
      strategy: secret_ref
      secret_ref: search-api-key

code_mode:
  disabled: false
  max_execution_bytes: 65536
  max_tool_calls_inside: 10
  require_approval_on_execute: true
  deny_unsafe_starlark: true

agent_profiles:
  - name: support-agent
    tenant: acme
    description: Read-only customer support agent
    allowed_mcp_servers: [internal-search]
    allowed_tools: [internal-search.search, internal-search.get_document]
    scopes: [mcp:call]
    bindings: [support-bot@acme]

cache:
  driver: redis
  scope: tenant
  ttl: 15m
  options:
    addr: redis:6379
    db: 1
```

---

## Validation errors

The loader returns typed `FieldError` values. Common messages:

| Error | Cause |
|---|---|
| `config: server.bind: is required` | `server.bind` is empty or missing. |
| `config: auth: is required when server.bind is not localhost` | `auth` block omitted for a non-loopback bind. |
| `config: auth.jwt.issuer: is required` | `auth.jwt.issuer` missing. |
| `config: auth.jwt: either jwks_url or static_jwks is required` | Neither JWKS source provided. |
| `config: auth.jwt: jwks_url and static_jwks are mutually exclusive` | Both JWKS sources provided. |
| `config: storage.driver: only 'sqlite' is supported in V1` | Unsupported storage driver. |
| `config: tenants[N].id: invalid format` | Tenant ID does not match `^[a-z0-9][a-z0-9_-]{0,63}$`. |
| `config: servers[N].transport: is required` | Transport field missing on a server entry. |
| `config: servers[N].stdio.command: is required when transport=stdio` | Stdio block absent for a stdio server. |
| `config: agent_profiles[N].tenant: is required when zero or multiple tenants are configured` | `tenant` omitted on a profile with ambiguous tenant context. |

---

## Related

- [Deployment Guide](/guides/deployment) — production deployment patterns, systemd unit, and Dockerfile.
- [Authentication](/concepts/authentication) — JWT validation, claim mapping, and dev mode in depth.
- [Credentials Vault](/concepts/credentials-vault) — vault file format, key generation, and rotation.
- [Semantic Cache](/concepts/semantic-cache) — caching architecture, driver comparison, and TTL strategy.
- [Observability](/concepts/observability) — OpenTelemetry integration, span retention, and the drift detector.
- [Agent Profiles](/concepts/agent-profiles) — profile lifecycle and the entitlement model.
- [Skill Packs](/concepts/skill-packs) — Skill Pack authoring and source configuration.
- [CLI Reference](/reference/cli) — all subcommands, including `vault` and `validate`.
