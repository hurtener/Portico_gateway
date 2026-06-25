---
layout: home

hero:
  name: "Portico"
  text: "One governed gateway for all your agentic traffic."
  tagline: "MCP, A2A, and LLM under one multi-tenant control plane — with Agent Profiles, Virtual Keys, budgets, audit, and a vault built in. One CGo-free Go binary. Open source."
  image:
    src: /portico-logo.svg
    alt: Portico
  actions:
    - theme: brand
      text: Get started
      link: /getting-started/
    - theme: alt
      text: Core concepts
      link: /concepts/
    - theme: alt
      text: View on GitHub
      link: https://github.com/hurtener/Portico_gateway

features:
  - title: Agent Profiles
    details: "The primitive operators actually think in. One named, tenant-scoped object binds a consumer to a curated set of MCP servers, tools, Skill Packs, LLM aliases, scopes, and Virtual Keys. It is the single source of truth for who sees what."
    link: /concepts/agent-profiles
  - title: MCP Gateway
    details: "Full MCP northbound (Streamable HTTP + SSE) and a per-tenant southbound fleet over stdio and HTTP servers. Namespaced tool/resource/prompt aggregation, hot reconfiguration, crash recovery."
    link: /concepts/mcp-gateway
  - title: LLM Gateway
    details: "An OpenAI-compatible surface over many providers, with model aliases, routing, and provider fallback. Drop it in front of your apps and govern every call."
    link: /concepts/llm-gateway
  - title: A2A (Agent-to-Agent)
    details: "Speak the A2A peer protocol on the same listener as MCP, through the same governance envelope. Discover agent cards, dispatch tasks, bridge MCP tools to A2A peers."
    link: /concepts/a2a
  - title: Virtual Keys & Budgets
    details: "Sub-divide a tenant into per-app, per-developer, per-environment keys — each with its own scopes, allowlists, and audit lineage. Hierarchical budgets nest VK → Team → Customer → Tenant."
    link: /concepts/virtual-keys
  - title: Credentials behind the gate
    details: "Agents never receive broad downstream tokens. The encrypted vault, OAuth 2.0 token exchange (RFC 8693), and credential injectors keep secrets on Portico's side of the line."
    link: /concepts/credentials-vault
  - title: Semantic Cache
    details: "Put a cache in front of the LLM gateway so repeated or near-repeated requests skip the upstream call entirely — cutting cost and latency. Tenant-isolated by construction."
    link: /concepts/semantic-cache
  - title: Code Mode
    details: "Let MCP clients orchestrate tools by writing sandboxed Starlark instead of shipping a 150-tool catalog into context. Fewer round trips, fewer tokens — every call still fully governed."
    link: /concepts/code-mode
  - title: Skill Packs
    details: "Bind the open Skills spec to specific servers, tools, policies, and UI resources — turning raw tools into reliable, governed workflows any compliant MCP client can consume."
    link: /concepts/skill-packs
  - title: Multi-tenant from V1
    details: "Tenant identity flows through every layer, from the JWT to every row of storage. Per-tenant process isolation, per-tenant vault, per-tenant audit. No single-tenant assumptions anywhere."
    link: /concepts/multi-tenancy
  - title: Observability & Audit
    details: "Structured logs, OpenTelemetry tracing, a redacting audit trail, and schema-drift detection across the fleet. Every governed decision is recorded and queryable."
    link: /concepts/observability
  - title: One binary, with a Console
    details: "A single static Go binary serves REST, MCP, A2A, and an embedded SvelteKit operator Console. SQLite by default — no Postgres, no Redis, no Kubernetes required to run."
    link: /concepts/console
---

## Install

Portico is a single CGo-free Go binary. Build it from source and run it locally
in seconds:

```bash
git clone https://github.com/hurtener/Portico_gateway
cd Portico_gateway
make build
./bin/portico dev   # binds 127.0.0.1:8080, synthesizes a `dev` tenant, no JWT
```

The Console is then at `http://localhost:8080/`. See the
[5-minute quickstart](/getting-started/) for the full walkthrough.

## Why Portico

MCP standardized how agents reach tools. The open Skills spec standardized how
agents are taught to use them. A2A standardized how agents talk to each other.
Production needs the layer in between — the one that decides **who** is allowed
to do **what**, injects credentials safely, meters spend, and records everything
that happened.

Portico is that layer. It speaks MCP, A2A, and an OpenAI-compatible LLM API
outward and inward, and puts every call through one governance envelope:

> **tenant → JWT / Virtual Key → Agent Profile → policy → audit → tracing**

Enterprises don't replace their HTTP gateway — they consolidate their *agentic*
traffic. Portico is built for exactly that consolidation, and ships the
governance, vault, audit, and multi-tenancy as open source from day one.

## The ecosystem

Portico is one product in a three-part family:

```text
Portico  — the MCP / A2A / LLM gateway   (connects and governs)
Harbor   — the agent framework           (builds and runs agents; owns the MCP client)
Dockyard — the MCP Apps framework        (builds the MCP servers and apps users touch)
```

> Portico connects. Harbor reasons. Dockyard presents.

## Where to go next

- **[Get started](/getting-started/)** — build the binary, boot dev mode, make your first governed tool call in five minutes.
- **[Concepts](/concepts/)** — the full map of what Portico does and how the pieces fit.
- **[Guides](/guides/)** — deploy with a real config, manage providers and keys, turn on Code Mode.
- **[Reference](/reference/configuration)** — configuration schema, roadmap, and the design RFC.
