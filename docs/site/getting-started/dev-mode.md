# Dev mode

Dev mode is a single-command way to run Portico locally without configuring JWT authentication, tenants, or a secrets vault. It is designed for the first fifteen minutes of evaluation, iterating on MCP server registrations, and local Skill Pack development — not for production or any network-exposed deployment.

## Starting the server

```bash
./bin/portico dev
```

Without flags, the server binds to `127.0.0.1:8080`, creates a SQLite database at `./portico.db` in the current working directory, and synthesizes a tenant named `dev`. The process writes structured text logs to stderr.

::: info Build first
`./bin/portico` is produced by `make build`. If you have not built yet, see [Installation](/getting-started/installation).
:::

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--bind` | `127.0.0.1:8080` | Address and port to listen on. Must be a loopback address (see [Safety guarantee](#safety-guarantee)). |
| `--data-dir` | *(cwd)* | Directory for `portico.db` and the local vault file. Created if absent. |

### Boot log

A successful start emits two `slog` lines to stderr in text format:

```
time=... level=INFO msg="portico boot" version=... commit=... bind=127.0.0.1:8080 dev_mode=true storage_driver=sqlite tenants_configured=0
time=... level=INFO msg="listening" bind=127.0.0.1:8080 tenant_id=dev
```

The `dev_mode=true` attribute confirms the auth bypass is active. `tenant_id=dev` is the synthesized tenant that every unauthenticated request is assigned to.

Once you see the `listening` line, the health endpoint is ready:

```bash
curl http://127.0.0.1:8080/healthz
# {"status":"ok"}
```

## How dev mode works

### The two conditions

Dev mode activates when **both** of the following are true:

1. `auth` is absent from the configuration (the `Auth` field is `nil`).
2. The bind address resolves to a loopback interface — `127.0.0.1`, `::1`, or `localhost`.

Portico evaluates this at `Config.IsDevMode()`. The check is intentionally a conjunction: omitting auth on a publicly routable address is rejected by config validation with an explicit error rather than silently allowed.

```
config: auth: is required when server.bind is not localhost (dev mode requires 127.0.0.1 / localhost bind)
```

### Synthesized dev identity

In dev mode the JWT validator is never constructed. The tenant authentication middleware, instead of requiring an `Authorization: Bearer <jwt>` header, injects a synthetic identity on every request:

| Field | Value |
|-------|-------|
| `tenant_id` | `dev` (or `$PORTICO_DEV_TENANT`) |
| `user_id` | `dev` |
| `plan` | `enterprise` |
| `scopes` | `["admin"]` |

The dev tenant is upserted into the database on the first request that hits the auth middleware, so it exists by the time any API call reaches a handler.

Because every request carries the `admin` scope, all management surfaces — server registry, audit, approvals, snapshots, skill sources, and the LLM gateway — are accessible without any extra configuration.

### Overriding the tenant name

Set `PORTICO_DEV_TENANT` before starting the server to use a different tenant ID:

```bash
PORTICO_DEV_TENANT=my-workspace ./bin/portico dev
```

This is useful when you want the dev database to carry a more meaningful name, or when running multiple isolated dev instances pointing at different data directories.

### Config hot-reload is disabled

`portico dev` synthesizes its configuration in memory and passes no file path to the config watcher. Hot-reload is a `serve --config` feature. If you change flags you must restart the process.

## Automatic Skill Pack loading

On startup, dev mode searches two directories for local Skill Packs and registers them as `local`-type skill sources:

1. `./skills` — your working directory, intended for packs under active development.
2. `./examples/skills` — the reference packs shipped with the repository.

If neither directory exists the skills runtime starts in a disabled state and the `/v1/skills` surface returns an empty catalog. No error is emitted; the rest of the gateway operates normally.

You can verify which sources loaded:

```bash
curl http://127.0.0.1:8080/v1/skills
```

## Safety guarantee

Portico enforces that the bind address is a loopback interface before entering dev mode. The check happens at two points:

1. `cmd/portico/cmd_dev.go` — `isLocalhostBind` rejects anything outside `127.0.0.1`, `::1`, and `localhost` with an immediate error before the server starts:

```
dev: bind must be 127.0.0.1, ::1, or localhost; got "0.0.0.0:8080"
```

2. `internal/config/loader.go` — `Config.Validate()` rejects a config that has `auth: nil` and a non-loopback bind, so even a config file constructed programmatically cannot pass validation in this state.

The result is that dev mode credentials — which grant `admin` scope with no authentication — cannot accidentally be exposed on a network interface.

## What routes bypass authentication

Health and readiness probes are always unauthenticated regardless of mode:

| Path | Response |
|------|----------|
| `GET /healthz` | `{"status":"ok"}` — HTTP 200 |
| `GET /readyz` | `{"status":"ready","version":"...","commit":"..."}` — HTTP 200 |

Console static assets under `/_app/` and a small set of public files (`/favicon.svg`, `/robots.txt`) are also exempt. Every other path goes through the auth middleware, which in dev mode injects the synthetic identity rather than validating a JWT.

## Contrast with `serve --config`

`portico dev` and `portico serve --config <path>` both call the same internal boot sequence. The differences are:

| Property | `dev` | `serve --config` |
|----------|-------|-----------------|
| Auth | No JWT required; synthetic `dev` identity injected | JWT validation required; `Authorization: Bearer <token>` on every request |
| Config source | In-memory, synthesized from flags | YAML file on disk |
| Hot-reload | Disabled | Enabled (fsnotify watcher re-seeds the registry on file change) |
| Bind restriction | Must be loopback | Any valid address |
| Tenant provisioning | Auto-upserted `dev` tenant | Tenants declared in YAML, seeded at boot |
| Logging format | `text` | `json` (unless overridden in YAML) |
| Skill sources | Auto-discovers `./skills` and `./examples/skills` | Declared under `skills.sources` in YAML |

A minimal `portico.yaml` for `serve` looks like this:

```yaml
server:
  bind: 0.0.0.0:8080

auth:
  jwt:
    issuer: https://your-idp.example.com/
    audiences: [portico]
    jwks_url: https://your-idp.example.com/.well-known/jwks.json

storage:
  driver: sqlite
  dsn: file:/var/lib/portico/portico.db?cache=shared

tenants:
  - id: acme
    display_name: Acme Corp
    plan: enterprise
    entitlements:
      skills: ["*"]
      max_sessions: 200

logging:
  level: info
  format: json

skills:
  sources:
    - type: local
      path: /opt/portico/skills
```

With this config:

```bash
./bin/portico serve --config portico.yaml
```

::: warning Dev mode is not a staging environment
Dev mode skips all authentication. It is appropriate for a single developer's local machine. For any shared or persistent environment — including internal staging — use `serve --config` with a real JWT issuer and real tenant declarations.
:::

## The preflight gate

`make preflight` uses `portico dev` internally. It boots the server on `127.0.0.1:18080` (or `$PORTICO_PREFLIGHT_PORT`), waits for `/healthz` to return 200, runs all phase smoke scripts, then tears down. The `PORTICO_DEV_TENANT` environment variable is set to `preflight` so the ephemeral tenant name does not collide with a developer's `dev` database in the same working directory.

```bash
make preflight
# equivalent to: bash scripts/preflight.sh
```

To skip the gate in a documented emergency:

```bash
PORTICO_PREFLIGHT_SKIP=1 git commit -m 'reason: ...'
```

The CI pipeline still runs the gate; a local skip never reaches `main`.

## Related

- [Installation](/getting-started/installation) — build the binary and verify the toolchain.
- [Your first MCP server](/getting-started/first-mcp-server) — register a downstream MCP server in dev mode.
- [Authentication](/concepts/authentication) — JWT validation, tenant claims, and Virtual Keys for production deployments.
- [Multi-tenancy](/concepts/multi-tenancy) — how tenant identity flows through every layer of the gateway.
- [Configuration reference](/reference/configuration) — full schema for `portico.yaml`.
- [CLI reference](/reference/cli) — all subcommands and global flags.
