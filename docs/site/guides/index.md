# Guides overview

Guides are task-oriented walkthroughs. Each one answers "how do I accomplish X?" with concrete
commands, config fragments, and expected outputs. They assume you have Portico running — if you
have not yet built and booted the binary, start with [Getting started](/getting-started/) first.

**Guides vs. Concepts.** The [Concepts section](/concepts/) explains what each part of Portico
is, how it works internally, and why it is designed the way it is. Guides pick up where concepts
leave off: they assume you understand the idea and want the steps to execute it. For any guide
below, the concept pages linked inside it provide the deeper "why" when you need it.

::: info Prerequisites
All guides assume `./bin/portico` is available on your `PATH` or referenced directly, and that
you can reach a running instance — either `portico dev` for local work or a deployed instance
for production tasks. Guides that issue REST calls assume you have a valid Bearer JWT or
Virtual Key unless dev mode is specified.
:::

---

## Available guides

### [Deployment and configuration](/guides/deployment)

Go from a local dev session to a production-ready deployment with a real `portico.yaml` config
file. Covers choosing a data directory and storage path, generating and mounting the vault master
key (`PORTICO_VAULT_KEY`), configuring the HTTP bind address and TLS, tuning the process
supervisor runtime modes, and running behind a reverse proxy. Also covers the `portico validate`
preflight check and how to read the startup log to confirm every subsystem initialized cleanly.

Relevant concepts: [Architecture](/concepts/architecture),
[Multi-tenancy](/concepts/multi-tenancy), [Credentials Vault](/concepts/credentials-vault),
[Observability](/concepts/observability).

---

### [Manage providers, keys, and budgets](/guides/manage-providers)

Wire LLM providers into Portico's gateway, define model aliases that abstract provider and
model-version details from callers, and apply hierarchical spend controls. Covers:

- Registering a provider in `portico.yaml` and confirming it appears in `GET /v1/models`.
- Creating model aliases and a weighted fallback chain.
- Issuing Virtual Keys (`POST /api/virtual-keys`) scoped to specific aliases and servers.
- Setting up a budget hierarchy — Virtual Key → Team → Customer → Tenant — and verifying that
  the pre-call check and post-call reconcile behave correctly.
- Rotating a Virtual Key (`POST /api/virtual-keys/{id}/rotate`) and understanding why the
  old secret is never recoverable.

Relevant concepts: [LLM Providers](/concepts/llm-providers), [LLM Routing](/concepts/llm-routing),
[Virtual Keys](/concepts/virtual-keys), [Hierarchical Budgets](/concepts/hierarchical-budgets),
[Semantic Cache](/concepts/semantic-cache).

---

### [Create an Agent Profile](/guides/create-agent-profile)

Define the named, tenant-scoped object that governs exactly what a consumer can see and do.
Covers building a profile from scratch via `POST /api/agent-profiles`, specifying
`allowed_mcp_servers`, `allowed_tools`, `allowed_skills`, `allowed_model_aliases`, and `scopes`,
then binding the profile to a Virtual Key so every request through that key is automatically
constrained. Also covers how to verify enforcement: confirming that `tools/list` returns only
the entitled subset and that calls to excluded tools return the correct entitlement error.

Relevant concepts: [Agent Profiles](/concepts/agent-profiles),
[Virtual Keys](/concepts/virtual-keys), [Policy](/concepts/policy),
[Authentication](/concepts/authentication).

---

### [Register an MCP server](/guides/register-mcp-server)

Add a downstream MCP server to Portico's per-tenant registry, confirm the tool catalog
aggregates, and verify that tools are callable through the governed northbound endpoint. Covers
both the Console's server form and the `POST /api/servers` REST call. Includes stdio transport
(subprocess invocation, env injection, optional command allowlist) and HTTP transport (base URL,
auth headers stored in the vault). Shows how to use the Console Playground to test a tool call
end-to-end and read the resulting audit event.

Relevant concepts: [MCP Registry](/concepts/mcp-registry),
[MCP Northbound](/concepts/mcp-northbound), [MCP Southbound](/concepts/mcp-southbound),
[Catalog & Sessions](/concepts/catalog-and-sessions), [Approvals](/concepts/approvals).

---

### [Build a Skill Pack](/guides/build-skill-pack)

Author a versioned Skill Pack that binds the open Skills spec to a specific set of MCP servers
and tools, with Portico-specific metadata for risk classification and approval gating. Covers
the manifest layout (`skill.yaml`, instruction files, `portico-binding.yaml`), validating the
manifest with `portico validate-skills`, loading it via a `LocalDir` skill source in
`portico.yaml`, and confirming the skill appears in the virtual `skill://` resource directory
and as a namespaced prompt through standard MCP primitives. Includes how to add an approval
trigger on a destructive tool and observe the approval flow from the Console.

Relevant concepts: [Skill Packs](/concepts/skill-packs), [Skill Sources](/concepts/skill-sources),
[Approvals](/concepts/approvals), [Policy](/concepts/policy),
[Agent Profiles](/concepts/agent-profiles).

---

### [Use Code Mode](/guides/use-code-mode)

Enable Code Mode in an MCP session to orchestrate tools with sandboxed Starlark instead of
receiving the full namespaced catalog. Covers sending the opt-in capability flag in `initialize`,
inspecting the four `mcp.*` meta-tools, fetching tool stubs with `mcp.listToolFiles` and
`mcp.readToolFile`, and executing a multi-step snippet with `mcp.executeToolCode`. Walks through
the full error taxonomy (`code_mode.unsafe_call`, `code_mode.compile_error`,
`code_mode.budget_exceeded`, `code_mode.tool_error`, `code_mode.approval_required`), the
per-execution budget defaults, and how the `tokens_saved_est` field in the result is calculated.

::: tip Governance is not relaxed
Every tool call inside a Code Mode snippet traverses the same governance envelope as a direct
`tools/call` — Agent Profile entitlement check, policy evaluation, credential injection, audit
event. Code Mode changes the presentation model; it does not bypass any gate.
:::

Relevant concepts: [Code Mode](/concepts/code-mode),
[Code Mode — Token Savings](/concepts/code-mode-savings), [Policy](/concepts/policy),
[Approvals](/concepts/approvals).

---

### [Set up an A2A peer](/guides/setup-a2a-peer)

Register an external A2A peer so that tasks can be dispatched to it through Portico's governed
envelope. Covers registering the peer in `portico.yaml` (or via `POST /api/a2a-peers`),
confirming the peer's agent card is ingested and appears in Portico's aggregated agent card at
`GET /a2a/.well-known/agent.json`, and verifying a task dispatch with `POST /a2a`. Also covers
configuring an A2A bridge on an Agent Profile so that a named MCP `tools/call` is transparently
forwarded to the peer as an A2A task, with no changes to the calling agent. Credential injection
for peer authentication is handled through the vault, not passed through from the inbound token.

Relevant concepts: [A2A](/concepts/a2a), [A2A Bridges](/concepts/a2a-bridges),
[Agent Profiles](/concepts/agent-profiles), [Credentials Vault](/concepts/credentials-vault),
[OAuth Token Exchange](/concepts/oauth-token-exchange).

---

## Choosing where to start

If you are deploying Portico for the first time, read [Deployment](/guides/deployment) before
any of the others — it establishes the config file and vault key that every other guide
assumes are present.

If you are integrating an existing MCP tool catalog, start with
[Register an MCP server](/guides/register-mcp-server), then
[Create an Agent Profile](/guides/create-agent-profile) to scope which consumers see which tools.

If you are reducing token spend on large tool catalogs, read
[Use Code Mode](/guides/use-code-mode) alongside the [Code Mode concept](/concepts/code-mode).

If you are wiring LLM providers or setting spend limits, go to
[Manage providers, keys, and budgets](/guides/manage-providers).

---

## Related

- [Getting started](/getting-started/) — build, boot, and make your first governed call before
  working through any guide.
- [Concepts overview](/concepts/) — the full map of what Portico does and how its pieces fit
  together; the "why" behind every guide's "how."
- [Configuration reference](/reference/configuration) — the complete `portico.yaml` schema,
  referenced throughout the deployment and provider guides.
- [REST API reference](/reference/rest-api) — every endpoint with request and response shapes;
  the guides issue concrete `curl` calls against these.
- [CLI reference](/reference/cli) — all `portico` subcommands and flags.
