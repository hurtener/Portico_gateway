<p align="center">
  <img src="./docs/Design/portico-logo-assets-v2/portico-logo-transparent-512.png" alt="Portico" width="180" />
</p>

<h1 align="center">Portico</h1>

<p align="center">
  <strong>One governed gateway for all your agentic traffic.</strong>
</p>

<p align="center">
  MCP, A2A, and LLM under a single multi-tenant control plane — with Agent Profiles,<br/>
  Virtual Keys, budgets, an encrypted vault, and a full audit trail built in.<br/>
  One CGo-free Go binary. Open source.
</p>

<p align="center">
  <a href="https://github.com/hurtener/Portico_gateway/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="Apache 2.0" /></a>
  <a href="https://github.com/hurtener/Portico_gateway/actions/workflows/ci.yml"><img src="https://github.com/hurtener/Portico_gateway/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://hurtener.github.io/Portico_gateway/"><img src="https://img.shields.io/badge/docs-online-success.svg" alt="Docs" /></a>
  <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8.svg" alt="Go 1.22+" />
  <img src="https://img.shields.io/badge/protocols-MCP%20%C2%B7%20A2A%20%C2%B7%20LLM-6f42c1.svg" alt="MCP · A2A · LLM" />
</p>

<p align="center">
  <a href="https://hurtener.github.io/Portico_gateway/">Docs</a>
  ·
  <a href="#portico-in-5-minutes">Quickstart</a>
  ·
  <a href="#what-portico-does">What it does</a>
  ·
  <a href="#the-ecosystem">Ecosystem</a>
  ·
  <a href="#contributing">Contributing</a>
</p>

---

AI agents reach tools over **MCP**, talk to each other over **A2A**, and call models
over an **OpenAI-compatible API**. Each of those is a wire format. None of them is a
control plane. Production needs the layer in between — the one that decides **who** is
allowed to do **what**, injects credentials without ever handing an agent a broad token,
meters spend, and records everything that happened.

**Portico is that layer.** It speaks MCP, A2A, and LLM both outward (to AI clients) and
inward (to your servers and providers), and puts every single call through one governance
envelope:

```text
tenant  →  JWT / Virtual Key  →  Agent Profile  →  policy  →  audit  →  tracing
```

It runs as a single static binary with SQLite by default — no Postgres, no Redis, no
Kubernetes required to start — and it is multi-tenant from the first line of code.

## Portico in 5 minutes

```bash
# 1. Build the binary (CGO_ENABLED=0, single static artifact)
git clone https://github.com/hurtener/Portico_gateway
cd Portico_gateway
make build

# 2. Boot dev mode — binds 127.0.0.1:8080, synthesizes a `dev` tenant, no JWT needed
./bin/portico dev
```

A successful boot logs:

```json
{"time":"...","level":"INFO","msg":"listening","bind":"127.0.0.1:8080","tenant_id":"dev"}
```

Now you have, on one port:

- **The operator Console** at <http://localhost:8080/> — servers, sessions, skills, tenants, secrets, policy, playground, Agent Profiles, and the LLM screens.
- **The MCP endpoint** at `POST /mcp` — register a downstream MCP server and its tools are aggregated, namespaced, and governed.
- **The LLM gateway** at `POST /v1/chat/completions` — an OpenAI-compatible surface over many providers, behind your Virtual Keys and budgets.
- **The A2A endpoint** at `POST /a2a` — discover agent cards and dispatch tasks to peers, through the same envelope.

Point any MCP client, OpenAI SDK, or A2A peer at it and every request is authenticated,
entitlement-checked, credential-injected, metered, and audited.

> **Full walkthrough:** the [5-minute quickstart](https://hurtener.github.io/Portico_gateway/getting-started/) registers a server, makes a governed tool call, and runs your first LLM completion.

## What Portico does

| | |
|---|---|
| **Agent Profiles** | The primitive operators actually think in. One named, tenant-scoped object binds a consumer to a curated set of MCP servers, tools, Skill Packs, LLM aliases, scopes, and Virtual Keys — the single source of truth for *who sees what*. |
| **MCP Gateway** | Full MCP northbound (Streamable HTTP + SSE, JSON-RPC 2.0, capability negotiation, list-changed, cancellation, progress). A per-tenant southbound fleet over stdio and HTTP servers with namespaced aggregation, hot reconfiguration, and crash recovery. |
| **LLM Gateway** | An OpenAI-compatible API over many providers, with model aliases, weighted routing, and provider fallback. Drop it in front of your apps and govern every call. |
| **A2A** | Speak the Agent-to-Agent protocol on the same listener as MCP, through the same governance envelope. Discover agent cards, dispatch tasks, and bridge MCP tools to A2A peers. |
| **Virtual Keys & Budgets** | Sub-divide a tenant into per-app / per-developer / per-environment keys, each with its own scopes, allowlists, and audit lineage. Hierarchical budgets nest Virtual Key → Team → Customer → Tenant. |
| **Credentials behind the gate** | Agents never receive broad downstream tokens. An encrypted vault (AES-256-GCM), OAuth 2.0 token exchange (RFC 8693), and credential injectors keep secrets on Portico's side of the line. |
| **Skill Packs** | Bind the open Skills spec to specific servers, tools, policies, and UI resources — turning raw tools into reliable, governed workflows any compliant MCP client can consume. |
| **Code Mode** | Let MCP clients orchestrate tools by writing sandboxed Starlark instead of shipping a 150-tool catalog into context — fewer round trips, fewer tokens, every call still fully governed. |
| **Semantic Cache** | Put a cache in front of the LLM gateway so repeated or near-repeated requests skip the upstream call entirely. Tenant-isolated by construction. |
| **Observability & Audit** | Structured logs, OpenTelemetry tracing, a redacting audit trail, and schema-drift detection across the fleet. Every governed decision is recorded and queryable. |

All of it is open source, and all of it is multi-tenant — tenant identity flows from the
JWT to every row of storage, with per-tenant process isolation, vault, and audit.

## Why a gateway, and why this one

Enterprises don't rip out their HTTP gateway to adopt agents — they keep Kong / Envoy /
Istio / their ALB, and **consolidate their *agentic* traffic** somewhere it can be governed.
That somewhere needs to be protocol-aware about MCP sessions, A2A tasks, and LLM calls; it
needs a consumer model richer than a flat API key; and it needs the enterprise controls —
audit, governance, a vault, budgets, multi-tenancy — to be table stakes rather than an
upsell.

Portico is built for exactly that consolidation, and ships those controls as open source
from day one. The **Agent Profile** is the piece that ties it together: instead of an
allowlist smeared across scopes, snapshots, skill enablement, and key configuration, an
operator describes a consumer once — *"this agent talks to github, jira, and slack, uses
the `code-review` Skill, and may call `gpt-4o`"* — and every gate reads from that one object.

## The ecosystem

Portico is one product in a three-part family:

```text
Portico  — the MCP / A2A / LLM gateway   (connects and governs)
Harbor   — the agent framework           (builds and runs agents; owns the MCP client)
Dockyard — the MCP Apps framework        (builds the MCP servers and apps users touch)
```

> Portico connects. Harbor reasons. Dockyard presents.

## Documentation

The full documentation site covers getting started, every concept in depth, task-oriented
guides, and the complete configuration / CLI / REST / protocol reference.

**Start here:** **https://hurtener.github.io/Portico_gateway/**

- [Get started](https://hurtener.github.io/Portico_gateway/getting-started/) — build, boot, and make your first governed call.
- [Concepts](https://hurtener.github.io/Portico_gateway/concepts/) — the full map of what Portico does and how the pieces fit.
- [Guides](https://hurtener.github.io/Portico_gateway/guides/) — deploy with a real config, manage providers and keys, turn on Code Mode.
- [Reference](https://hurtener.github.io/Portico_gateway/reference/configuration) — configuration schema, CLI, REST API, and protocol methods.

## Build, test, run

```bash
make build                # CGo-free static binary -> ./bin/portico
make vet test             # go vet + go test -race
make lint                 # golangci-lint
make preflight            # build + boot + HTTP smoke against every implemented surface
make docs                 # build the VitePress documentation site

./bin/portico dev                          # local dev (loopback, synthetic `dev` tenant)
./bin/portico serve --config portico.yaml  # run with a real config
./bin/portico validate --config portico.yaml
```

`make preflight` is the live gate: it builds the binary, boots `portico dev`, waits for
`/healthz`, then runs each `scripts/smoke/*.sh`. The same gate runs in CI and from the
pre-commit hook (`make install-hooks`).

## Contributing

Portico is built doc-first. The RFC, phase plans, and architectural decisions live in the
repository — if you change the shape of the system, update the design record in the same PR.

Human and AI contributors should read:

- [`CLAUDE.md`](CLAUDE.md) / [`AGENTS.md`](AGENTS.md) — binding contributor and agent normatives (multi-tenant invariants, security rules, lint policy, the preflight contract).
- [`RFC-001-Portico.md`](RFC-001-Portico.md) — product intent and locked-in design decisions.
- [`docs/plans/`](docs/plans/) — implementation specs; acceptance criteria are binding.

Before opening a PR:

```bash
make preflight
```

## License

[Apache-2.0](LICENSE). See [`NOTICE`](NOTICE) for attribution.
