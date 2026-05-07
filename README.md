# Portico

> Multi-tenant MCP gateway and Skill runtime — single static Go binary, MCP-first.

Portico lets AI clients connect to many MCP servers through one governed, multi-tenant, session-aware control plane — and packages those servers with **Skill Packs** that turn raw tools into reliable workflows.

The gateway is the substrate. The Skill Pack runtime binds the open Skills spec to specific MCP servers, tools, policies, entitlements, and UI resources, and exposes the result through native MCP primitives so any compliant client benefits.

## What works today

- **Multi-tenant from the ground up.** JWT auth (RS256/ES256 family only) flows tenant + user identity through every layer; every tenant-scoped table is filtered by `tenant_id`. Dev mode synthesizes a `dev` tenant for local work.
- **Full MCP northbound (spec `2025-11-25`).** Streamable HTTP + SSE transport, JSON-RPC 2.0, capability negotiation, list-changed notifications, cancellation, progress, `_meta`, Origin 403 enforcement, SSE event IDs.
- **MCP southbound fleet.** Per-tenant client manager over stdio and HTTP downstream MCP servers; idle timeouts; crash recovery; namespaced tool/resource/prompt aggregation (`{server}.{name}`).
- **Server registry + lifecycle.** REST CRUD at `/v1/servers/*`, hot reconfiguration, list-changed propagation; servers persisted in SQLite.
- **Resources, prompts, MCP Apps.** Aggregated `resources/list`, `resources/read`, `prompts/list`, `prompts/get` across the fleet; `ui://` MCP Apps wrapped with the configured CSP.
- **Skill Pack runtime.** Manifest schema (`skills/v1`) with JSON Schema 2020-12 validation; `LocalDir` source with `fsnotify` hot-reload (200ms debounce); per-tenant/session enablement; synthetic `skill://` resources + namespaced prompts so vanilla MCP clients can consume skills directly. Four reference packs ship in `examples/skills/`.
- **Skill sources first-class (Phase 8).** Add Git/HTTP feeds or compose authored Skill Packs from the Console at runtime — no rebuild, no restart. Per-tenant `tenant_skill_sources` rows materialize through a driver registry (`internal/skills/source/{git,http,authored}`); authored packs land in SQLite with versioning + content checksums + draft/publish lifecycle. Console screens at `/skills/sources` and `/skills/authored` cover the full CRUD; the validation pipeline returns JSON-Pointer-tagged violations for inline highlighting.
- **Console CRUD (Phase 9).** Servers, tenants, secrets, and policy rules are first-class operator surfaces — the operator runs Portico once and manages the system from the browser. `/api/servers/*` adds `PATCH`, `restart`, `logs` (SSE), `health`, `activity`; `/api/admin/tenants/*` adds full create/update/archive/purge with runtime mode + JWT issuer fields; `/api/admin/secrets/*` covers CRUD plus a two-step reveal flow with one-shot, single-use, 60-second tokens; `/api/policy/rules/*` ships a SQL-backed editor with a dry-run evaluator. Every write emits an audit event with redacted before/after, lands in the `entity_activity` projection, and triggers hot-reload everywhere (no binary restarts). Named scopes (`servers:write`, `secrets:write`, `policy:write`, `tenants:admin`) gate writes; the umbrella `admin` scope is a wildcard for back-compat.
- **Console.** SvelteKit SPA (`adapter-static`, embedded via `//go:embed`). Pages for servers, sessions, resources, prompts, MCP Apps, skills, tenants, secrets, and the policy editor. Design tokens centralized in `web/console/src/lib/tokens.css`; typed API client at `web/console/src/lib/api.ts`.
- **Storage.** SQLite (`modernc.org/sqlite`, CGo-free) behind a `Backend` interface so future drivers slot in without touching callers.
- **Telemetry.** Structured `slog` everywhere with `tenant_id` / `request_id` / `session_id` / `server_id` attributes; OpenTelemetry hooks wired but tracing instrumentation expands in the next milestone.
- **Quality gates.** `gofmt`/`goimports`, `golangci-lint`, `go vet`, `go test -race`, `govulncheck`, secret scan, YAML/Markdown lint, frontend `svelte-check + build`, and a live preflight that boots the binary and runs HTTP smoke checks against every implemented surface — all enforced by CI and the local pre-commit hook.

## What's next

- **Playground (Phase 10).** Interactive MCP playground with catalog browser, schema-driven tool composer, and live trace + audit + drift correlation. See [`docs/plans/phase-10-playground.md`](docs/plans/phase-10-playground.md).

## Quickstart

```bash
# One-time
make install-hooks

# Build the static binary (CGO_ENABLED=0)
make build

# Run in dev mode (binds 127.0.0.1:8080, synthesizes the `dev` tenant, no JWT required)
./bin/portico dev

# Run with a real config
./bin/portico serve --config portico.yaml

# Validate config / skills
./bin/portico validate --config portico.yaml
./bin/portico validate-skills ./examples/skills/...
```

A successful boot logs:

```json
{"time":"...","level":"INFO","msg":"listening","bind":"127.0.0.1:8080","tenant_id":"dev"}
```

The Console is at <http://localhost:8080/> and shows registered servers, sessions, resources, prompts, MCP Apps, and skills.

## Tests, lint, preflight

```bash
make vet test build       # core gates
make lint                 # golangci-lint v1.64.x
make preflight            # build + boot + HTTP smoke against every implemented surface
```

`make preflight` is the live gate: it builds `./bin/portico`, boots `./bin/portico dev` on `127.0.0.1:18080`, waits for `/healthz`, then runs each `scripts/smoke/*.sh`. The same gate runs in CI and from the pre-commit hook. Smoke scripts auto-skip surfaces that aren't implemented yet, so adding a new endpoint without a smoke check is a rejection-on-sight reason.

## V1 scope

- Multi-tenant by JWT, dev-mode bypass for local.
- Full MCP spec including resources, prompts, sampling, roots, elicitation, list-changed, cancellation, progress, `_meta`, plus MCP Apps (`ui://`) with CSP enforcement.
- Skill Pack runtime — manifest + JSON Schema validator; virtual-directory loader (`LocalDir` in V1; Git/OCI/HTTP post-V1); skills exposed as `skill://` resources and namespaced prompts.
- Headless approval flow — elicitation when host supports it, structured error otherwise.
- Process supervisor with five runtime modes: `shared_global`, `per_tenant`, `per_user`, `per_session`, `remote_static`.
- Credential vault — file-encrypted (AES-256-GCM, HKDF per value), OAuth 2.0 token exchange (RFC 8693), env / header injection.
- Catalog snapshots stable per session; live updates opt-in.
- OpenTelemetry tracing across gateway, runtime, skills, southbound calls.
- SQLite by default; Postgres post-V1.

## What it explicitly is not (V1)

- Not a hosted SaaS — local and self-hosted only.
- Not Kubernetes-native — deployment artifacts post-V1.
- Not a sandboxed runtime — V1 uses plain subprocesses with optional Linux seccomp/landlock; container/microVM isolation is post-V1.
- Not a replacement for MCP or Skills — Portico extends both.

## Repo layout

```
portico/
  cmd/portico/             # main binary, subcommands (dev/serve/validate/validate-skills)
  internal/
    auth/                  # JWT, tenant, scope
    config/                # schema + loader
    mcp/{protocol,northbound,southbound}/
    registry/              # MCP server registry
    runtime/               # process supervisor, sessions
    catalog/{namespace,resolver,snapshots}/
    skills/{manifest,source,loader,runtime}/
    apps/                  # ui:// indexer + CSP
    secrets/               # vault scaffolding (full impl in next milestone)
    server/{api,mcpgw,ui}/
    storage/{ifaces,sqlite}/
    telemetry/
  web/console/             # SvelteKit SPA, embedded via //go:embed
  examples/
    servers/mock/          # in-process + standalone mock MCP servers
    skills/                # 4 reference Skill Packs
  scripts/smoke/           # phase-N HTTP smoke checks
  test/integration/
  docs/
    plans/                 # implementation plans
  RFC-001-Portico.md
  AGENTS.md / CLAUDE.md    # contributor + agent normatives (verbatim mirrors)
```

## Authoritative sources

1. [`RFC-001-Portico.md`](RFC-001-Portico.md) — design intent and locked-in decisions.
2. [`docs/plans/`](docs/plans/) — implementation specs; acceptance criteria are binding.
3. [`AGENTS.md`](AGENTS.md) / [`CLAUDE.md`](CLAUDE.md) — contributor and agent normatives (multi-tenant invariants, security rules, lint policy, preflight contract).

## License

TBD (open source intent).
