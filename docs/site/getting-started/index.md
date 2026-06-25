# Overview

Portico is a multi-tenant MCP gateway and Skill runtime — a single static Go binary that puts
every AI client request through one governed control plane: **MCP, A2A, and an
OpenAI-compatible LLM API all on the same listener, with the same vault, the same audit trail,
and the same tenant isolation**.

The gateway is the substrate. The governance envelope that wraps every call is the product:

```
tenant → JWT / Virtual Key → Agent Profile → policy → audit → tracing
```

No request reaches a downstream MCP server, an LLM alias, or an A2A peer until it has passed
through all five layers of that envelope.

---

## What Portico does

### MCP gateway

Portico presents a single MCP endpoint (full spec `2025-11-25`, Streamable HTTP + SSE) to AI
clients and maintains a per-tenant fleet of outbound MCP connections to downstream servers.
Every tool, resource, and prompt exposed by those servers is aggregated under a namespaced
catalog (`{server}.{tool}`) and kept alive across the fleet, with crash recovery and hot
reconfiguration. Clients see one MCP server; Portico manages the rest.

See [MCP Gateway](/concepts/mcp-gateway) for the full architecture, and
[Northbound](/concepts/mcp-northbound) / [Southbound](/concepts/mcp-southbound) for transport
details.

### LLM gateway

An OpenAI-compatible surface (`/v1/chat/completions`, `/v1/models`, …) sits in front of
multiple LLM providers. Operators define model aliases that abstract provider and model-version
details; clients use the alias. Every call is metered, cached (optionally), and governed through
the same Agent Profile and budget machinery that governs MCP calls. No separate config file,
no separate process.

See [LLM Gateway](/concepts/llm-gateway), [Providers](/concepts/llm-providers),
[Routing](/concepts/llm-routing), and [Semantic Cache](/concepts/semantic-cache).

### A2A (agent-to-agent)

Portico speaks the A2A peer protocol on the same listener as MCP. It can act as an A2A server
(advertising an agent card, accepting task dispatches) and as an A2A client (forwarding tool
calls to peer agents discovered in its registry). Bridges between the MCP tool surface and A2A
peers are first-class configuration objects, not code changes.

See [A2A](/concepts/a2a) and [A2A Bridges](/concepts/a2a-bridges).

### Skill Packs

A Skill Pack is a versioned package that binds the open Skills spec to specific MCP servers,
tools, policies, credentials, and UI resources. Skills tell agents when and how to use a tool;
Skill Packs enforce that guidance at the runtime layer, with approval gating for dangerous
operations and rich binding metadata surfaced to Portico-aware clients. Vanilla MCP clients
benefit too — Skills are exposed as `skill://` resources and namespaced prompts through
standard MCP primitives.

See [Skill Packs](/concepts/skill-packs) and [Skill Sources](/concepts/skill-sources).

### Code Mode

Code Mode is an opt-in MCP session projection where the client writes sandboxed Starlark to
orchestrate tools rather than calling them one by one. A 200-tool catalog collapses to four
meta-tools in context; intermediate results stay inside the sandbox; every tool call the script
makes still traverses the same full governance envelope. The result is fewer context tokens and
fewer round trips, with no relaxation of policy or audit.

See [Code Mode](/concepts/code-mode) and [Code Mode savings](/concepts/code-mode-savings).

---

## The governance envelope in detail

Every inbound request — MCP, LLM, or A2A — passes through a fixed middleware chain before
any work is dispatched:

| Layer | What it does |
|---|---|
| **Tenant resolver** | Extracts tenant identity from the Bearer JWT (`tenant` claim) or from the Virtual Key prefix. Dev mode synthesizes a `dev` tenant and skips JWT validation. |
| **Auth** | Validates the JWT (RS256/ES256 family; HS\* and `none` are forbidden) or verifies the Virtual Key's HMAC-SHA256. Returns `401` on failure. |
| **Virtual Key** | If auth was via VK, intersects the VK's own server/tool/model allowlists with the bound Agent Profile. Stores only a `salt` + HMAC — the secret is returned once and never again. |
| **Agent Profile** | Resolves the named profile bound to this principal (JWT subject or VK). Writes `allowed_mcp_servers`, `allowed_tools`, `allowed_skills`, `allowed_model_aliases`, and `scopes` into the request context. Principals with no profile bound see the tenant's full surface — back-compat is explicit. |
| **Policy** | Evaluates the request against per-tenant policy rules. Tools marked `requires_approval` emit an `elicitation/create` request or a structured `approval_required` error — Portico never silently bypasses them. |
| **Audit** | Records a redacted event for every governed decision: tool call, policy decision, Skill activation, credential injection, drift detection hit. Queryable per tenant. |
| **Tracing** | Injects trace context (`traceparent`) into northbound spans, southbound HTTP headers, and stdio server env vars. OTel hooks are wired throughout. |

Intersection semantics apply everywhere in the stack: a Profile may restrict but never widen a
scope carried in the JWT; a Virtual Key may restrict but never widen the Profile it is bound to.
Most-restrictive wins at every layer.

---

## Single-binary story

Portico is one CGo-free, statically linked Go binary with no external runtime dependencies:

- **Storage:** SQLite via `modernc.org/sqlite` (pure Go). No Postgres, no Redis required to
  run. The `Backend` interface means alternative drivers slot in without touching any caller
  code; Postgres is planned post-V1.
- **Console:** A SvelteKit SPA embedded via `//go:embed`. Opening `http://localhost:8080/`
  after `./bin/portico dev` serves the full operator Console from the same process.
- **Build flag:** `CGO_ENABLED=0`. The CI gate verifies this on every push.

One `make build` produces the binary. No Docker required for local development, though a
Dockerfile is included for container deployments.

---

## What you need

Before you begin, make sure the following are available on your workstation:

- **Go 1.22 or later.** Portico uses standard library features and generics from 1.22+. Verify
  with `go version`.
- **GNU Make.** The `Makefile` wraps all build, test, and preflight targets. Available on
  macOS (`xcode-select --install`), Linux (package manager), and Windows (WSL or Git Bash).
- **Git.** To clone the repository.
- **Node.js 20+ and npm.** Required only if you intend to modify the Console (`web/console/`).
  The binary build embeds the pre-built Console assets; frontend tooling is not needed for
  Go-only work.

No Postgres, Redis, Docker, or Kubernetes instance is required for local development or
the steps below.

---

## Five-minute path

The steps below take a fresh checkout to a running Portico instance with a governed tool call
confirmed. Each step is covered in a dedicated page:

### 1. Install

Clone the repository, run `make build`, and optionally install the pre-commit hook that
enforces the same quality gate as CI:

```bash
git clone https://github.com/hurtener/Portico_gateway
cd Portico_gateway
make install-hooks   # one-time; wires the pre-commit preflight
make build           # CGO_ENABLED=0; produces ./bin/portico
```

Full details, including binary placement and CI-pinned linter versions, are on the
[Installation](/getting-started/installation) page.

### 2. Boot dev mode

Dev mode starts Portico on `127.0.0.1:8080`, synthesizes a `dev` tenant, and skips JWT
validation — safe for local work, never for production:

```bash
./bin/portico dev
```

A successful boot logs a single JSON line:

```json
{"time":"...","level":"INFO","msg":"listening","bind":"127.0.0.1:8080","tenant_id":"dev"}
```

The Console is live at `http://localhost:8080/` and the MCP endpoint is at
`http://localhost:8080/mcp`. Health check: `GET /healthz` returns `200 OK`.

Full details are on the [Dev mode](/getting-started/dev-mode) page.

### 3. Register your first MCP server

Register a downstream MCP server through the Console or the REST API, verify the tool catalog
aggregates, and make a governed `tools/call`:

```bash
curl -s -X POST http://localhost:8080/api/servers \
  -H 'Content-Type: application/json' \
  -d '{"name":"echo","transport":"stdio","command":"npx","args":["-y","@example/echo-mcp"]}'
```

End-to-end walkthrough on [Your first MCP server](/getting-started/first-mcp-server).

### 4. Make your first LLM call

Configure a model alias pointing to a provider, then call the OpenAI-compatible surface:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"default","messages":[{"role":"user","content":"hello"}]}'
```

Provider wiring and alias configuration are covered on
[Your first LLM call](/getting-started/first-llm-call).

### 5. Tour the Console

The embedded operator Console gives you a live view of registered servers, active sessions,
tool call history, policy decisions, and the audit log — all tenant-scoped. The
[Console tour](/getting-started/console-tour) walks through each section.

---

## Architecture in one diagram

```text
                AI client / IDE / agent framework
                              |
              ┌───────────────▼───────────────────┐
              │           Portico                  │
              │  ┌─────────────────────────────┐   │
              │  │ Tenant resolver (JWT / VK)  │   │
              │  │ Agent Profile + policy      │   │
              │  │ Approval flow               │   │
              │  │ Credential vault + injector │   │
              │  │ Audit + tracing             │   │
              │  └─────────────────────────────┘   │
              │                                     │
              │  MCP northbound │ LLM /v1 │ A2A    │
              └──────┬──────────┴──────────┴───────┘
                     │
        ┌────────────┼───────────────────┐
        ▼            ▼                   ▼
   GitHub MCP   Postgres MCP        LLM provider
   (stdio,      (http,              (aliased via
    per_user)    per_tenant)         portico.yaml)
```

For the full architecture description, see [Architecture](/concepts/architecture) and the
[design RFC](/reference/rfc).

---

## Next steps

| What you want to do | Where to go |
|---|---|
| Build the binary and install the pre-commit hook | [Installation](/getting-started/installation) |
| Boot a local dev instance | [Dev mode](/getting-started/dev-mode) |
| Register a downstream MCP server and make a tool call | [Your first MCP server](/getting-started/first-mcp-server) |
| Wire a provider and call the LLM gateway | [Your first LLM call](/getting-started/first-llm-call) |
| Navigate the embedded operator Console | [Console tour](/getting-started/console-tour) |
| Understand the full architecture | [Architecture](/concepts/architecture) |
| Learn about multi-tenancy | [Multi-tenancy](/concepts/multi-tenancy) |
| Create an Agent Profile to scope a consumer | [Agent Profiles](/concepts/agent-profiles) |
| Issue a Virtual Key for a per-app credential | [Virtual Keys](/concepts/virtual-keys) |
| Deploy to a real server with a config file | [Deployment](/guides/deployment) |
| Read the design RFC | [RFC-001](/reference/rfc) |

::: tip Already familiar with MCP?
If you have used MCP clients before, skip to [Your first MCP server](/getting-started/first-mcp-server)
and use the [Reference: MCP methods](/reference/mcp-methods) as a reference alongside it.
:::

::: info SQLite is the default
No database setup is required. Portico creates its SQLite file in the data directory on first
boot (`~/.portico/` in dev mode, or the path set by `storage.path` in `portico.yaml`). Postgres
support is on the [roadmap](/reference/roadmap) for post-V1.
:::
