# Portico

> An MCP gateway and Skill runtime — multi-tenant from V1, single static Go binary, MCP-first.

**Status: pre-V1, in design.** The RFC and phased implementation plans live in [`docs/`](docs/). No code yet.

## What it is

Portico lets AI clients connect to many MCP servers through one governed, multi-tenant, session-aware control plane — and packages those servers with **Skill Packs** that turn raw tools into reliable workflows.

The gateway is the substrate. The Skill Pack runtime is the moat: it binds the open Skills spec to specific MCP servers, tools, policies, entitlements, and UI resources, and exposes the result through native MCP primitives so any compliant client benefits.

## Why

MCP solved access. Skills solved teaching. Nothing yet solves the runtime in between — the layer that manages many MCP servers per tenant, binds Skills to specific servers/tools/policies, resolves what each session is entitled to, injects credentials safely without leaking tokens to agents, audits everything with stable catalog snapshots, and isolates tenants by process, storage, credentials, and audit. Portico is that runtime.

## What's here today

- [`RFC-001-Portico.md`](RFC-001-Portico.md) — the design RFC (v3).
- [`docs/plans/`](docs/plans/) — seven self-contained implementation plans (Phase 0 through Phase 6) that, in order, take an empty repo to V1.
- [`docs/plans/README.md`](docs/plans/README.md) — index + cross-cutting conventions.

## Quickstart (post-implementation; here for the shape it will take)

```bash
# Build
make build

# Run in dev mode (binds to 127.0.0.1, synthesizes a `dev` tenant)
./bin/portico dev

# Or with a real config (production)
./bin/portico serve --config portico.yaml
```

A successful boot prints a JSON line like:

```json
{"time":"...","level":"INFO","msg":"listening","bind":"127.0.0.1:8080","tenant_id":"dev"}
```

The Console at `http://localhost:8080/` shows registered servers, skills, sessions, approvals, and audit events.

## V1 scope at a glance

- **Multi-tenant** from V1 (JWT with `tenant` claim; dev-mode bypass for local).
- **Full MCP spec** including resource templates, prompts, sampling, roots, elicitation, list-changed, cancellation, progress, `_meta`, plus **MCP Apps (`ui://`)** with CSP enforcement.
- **Skill Pack runtime** — manifest + JSON Schema validator; virtual-directory loader (`LocalDir` in V1; Git/OCI/HTTP post-V1); skills exposed as `skill://` MCP resources and prompts so vanilla clients benefit too.
- **Headless approval flow** — `elicitation/create` when host supports it, structured `approval_required` error otherwise.
- **Process supervisor** — five runtime modes (`shared_global`, `per_tenant`, `per_user`, `per_session`, `remote_static`), idle timeout, crash recovery, env interpolation, log capture.
- **Credential vault** — file-encrypted (AES-256-GCM, HKDF per value); OAuth 2.0 token exchange (RFC 8693); env / header injection strategies.
- **Catalog snapshots** stable per session by default; live updates opt-in.
- **OpenTelemetry** tracing across gateway, runtime, skills, southbound calls.
- **SQLite** by default; Postgres post-V1.

## What it explicitly is not (V1)

- Not a hosted SaaS. Local and self-hosted only in V1.
- Not Kubernetes-native yet (deployment artifacts post-V1).
- Not a sandboxed runtime — V1 uses plain subprocesses with optional Linux seccomp/landlock; container/microVM isolation is post-V1.
- Not a replacement for MCP or Skills. Portico extends both.

## Repo layout (target)

```
portico/
  cmd/portico/             # main binary, subcommands
  internal/                # implementation (Phase plans)
  web/console/             # embedded htmx + Templ UI
  examples/
    servers/               # mock + reference MCP server configs
    skills/                # 4 reference Skill Packs
  docs/
    plans/                 # phase implementation plans
  test/integration/
  RFC-001-Portico.md
  README.md
```

## License

TBD (open source intent).
