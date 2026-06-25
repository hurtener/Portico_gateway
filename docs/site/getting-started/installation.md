# Installation

Portico ships as a single static binary with no runtime dependencies. There is no Postgres, Redis, or Kubernetes required to run a production instance: the default storage backend is an embedded SQLite database (via `modernc.org/sqlite`, a pure-Go, Apache-2.0 driver), and every other subsystem — the MCP gateway, the A2A transport, the LLM gateway, the Skill runtime, the credential vault — is contained within that one binary.

This page covers building from source, optional Docker packaging, and verifying your installation.

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.22 or later | The module currently uses `go 1.25.5`; matching the toolchain is recommended |
| Git | any recent | For cloning the repository |
| `golangci-lint` | v1.61 or later | Only required to run `make lint`; not needed for `make build` |

::: info No CGo
The entire build is `CGO_ENABLED=0`. No C toolchain, no system SQLite, no CGo-linked dependencies of any kind are required.
:::

---

## Build from source

```bash
git clone https://github.com/hurtener/Portico_gateway.git
cd Portico_gateway
make build
```

`make build` runs:

```bash
CGO_ENABLED=0 go build \
  -tags 'sqlite_omit_load_extension' \
  -ldflags '-s -w' \
  -o bin/portico \
  ./cmd/portico
```

The result is `./bin/portico` — a fully self-contained binary that embeds the Console SPA, the SQLite engine, all protocol handlers, and the Skill runtime. Nothing else is needed to serve traffic.

### What gets built

- **`./bin/portico`** — the gateway binary (produced by `make build`)
- **`./bin/mockmcp`** — a standalone mock MCP server used in integration tests (produced by `make mockmcp`; optional for normal operation)

### Other build targets

```bash
make test       # run the full test suite with the race detector
make vet        # go vet ./...
make lint       # golangci-lint run ./... (requires golangci-lint v1.61+)
make preflight  # build, boot dev server, run HTTP smoke tests, tear down
make docker     # build the distroless Docker image (see below)
make clean      # remove ./bin/ and build artifacts
```

::: tip Run preflight before pushing
`make preflight` is the same gate that CI and the pre-commit hook enforce. It builds the binary, boots it, waits for `/healthz`, runs the smoke scripts, and tears down. Run it at least once before opening a pull request.
:::

---

## Verify the build

```bash
./bin/portico --help
```

Expected output (truncated):

```
Usage: portico <command> [flags]

Commands:
  serve     --config <path>           Run with a YAML config (production).
  dev       [--bind <addr>] [--data-dir <path>]
                                       Run in dev mode (localhost only).
  validate  --config <path>           Validate a config file and exit.
  validate-skills <path>...           Validate one or more Skill Pack manifests.
  vault     <subcommand>              Manage the credential vault.
  inspect-session <session_id> [--output json|table] [--since <RFC3339>]
                                       Dump a session's snapshot, audit events, approvals.
  conformance --suite openai --target <url> [--token <jwt>] [--model <alias>]
                                       Run OpenAI API conformance checks.
  code-mode render|exec               Starlark sandbox utilities.
  agents    list|get|create|delete|bind|unbind
                                       Manage agent profiles (offline).
  governance customers|teams          Manage governance objects (offline).
  version                             Print version info.

Run 'portico <command> -h' for command-specific flags.
```

Print version info:

```bash
./bin/portico version
```

---

## First run: dev mode

Dev mode is the fastest path from binary to running gateway. It binds `127.0.0.1:8080` by default, creates an in-process synthetic tenant called `dev`, and requires no JWT configuration.

```bash
./bin/portico dev
```

With a custom bind address or a persistent data directory:

```bash
./bin/portico dev --bind 127.0.0.1:9090 --data-dir /var/lib/portico-dev
```

::: warning Dev mode is localhost-only
Portico enforces that the `--bind` address resolves to `127.0.0.1`, `::1`, or `localhost` when running in dev mode. Any non-loopback address is rejected at startup. This is intentional: dev mode disables JWT validation, so binding it to a network interface would expose an unauthenticated surface.
:::

Once the binary is running, open `http://127.0.0.1:8080` to reach the embedded Console, or hit `http://127.0.0.1:8080/healthz` to confirm the gateway is alive.

---

## Production mode

For production, provide a `portico.yaml` configuration file:

```bash
./bin/portico serve --config /etc/portico/portico.yaml
```

The binary reads the config, opens (or creates) the SQLite database, seeds tenants, starts the MCP gateway, and begins listening on the configured bind address. Graceful shutdown is handled automatically on `SIGTERM` or `SIGINT`.

Validate a config file without starting the server:

```bash
./bin/portico validate --config /etc/portico/portico.yaml
```

See [Configuration reference](/reference/configuration) for the full `portico.yaml` schema.

---

## Docker

The repository ships a distroless multi-stage `Dockerfile`. Build the image locally:

```bash
make docker
# equivalent to: docker build -t portico/portico:dev .
```

The multi-stage build compiles the binary in a Go builder stage (with `CGO_ENABLED=0`) and copies only the resulting static binary into a minimal distroless base image. The final image has no shell, no package manager, and no build toolchain.

Run the image in dev mode:

```bash
docker run --rm -p 8080:8080 portico/portico:dev dev --bind 0.0.0.0:8080
```

::: info Dev mode inside Docker
When running in Docker you must explicitly pass `--bind 0.0.0.0:8080` (or another non-loopback address) and set `PORTICO_SKIP_LOCALHOST_CHECK=1`, or use a proper `portico.yaml` with `serve` mode. Dev mode's loopback restriction exists to prevent accidental public exposure; Docker networking already provides an isolation boundary.
:::

For production images, mount your `portico.yaml` and data directory as volumes, and use `serve --config`:

```bash
docker run --rm \
  -p 8080:8080 \
  -v /etc/portico:/etc/portico:ro \
  -v /var/lib/portico:/var/lib/portico \
  portico/portico:dev \
  serve --config /etc/portico/portico.yaml
```

---

## No external infrastructure required

Portico V1 is intentionally self-contained:

| Component | V1 default | Notes |
|---|---|---|
| Storage | Embedded SQLite (`modernc.org/sqlite`) | WAL mode, migrations run at boot |
| Credential vault | File-based vault (`PORTICO_VAULT_KEY`) | AES-256 key from env; no HashiCorp Vault required |
| LLM semantic cache | In-memory (`driver: inmem`) | Redis driver available; disabled by default |
| MCP transport | HTTP + SSE | No WebSocket or stdio northbound in V1 |
| Skill sources | Local directory (`type: local`) | Git and HTTP sources also available |

There is no Postgres, no Redis (unless you opt into the Redis semantic-cache driver), no message broker, and no Kubernetes operator. A single VM or bare-metal host running the binary is a fully functional production deployment.

---

## Next steps

- [Dev mode walkthrough](/getting-started/dev-mode) — tour the gateway in a running dev instance
- [Register your first MCP server](/getting-started/first-mcp-server) — connect a downstream MCP server
- [Configuration reference](/reference/configuration) — full `portico.yaml` schema
- [CLI reference](/reference/cli) — every subcommand and flag
- [Architecture overview](/concepts/architecture) — how the pieces fit together
- [Deployment guide](/guides/deployment) — production hardening, TLS, systemd, Docker Compose

---

## Related

- [/getting-started/dev-mode](/getting-started/dev-mode)
- [/getting-started/first-mcp-server](/getting-started/first-mcp-server)
- [/reference/configuration](/reference/configuration)
- [/reference/cli](/reference/cli)
- [/concepts/architecture](/concepts/architecture)
- [/guides/deployment](/guides/deployment)
