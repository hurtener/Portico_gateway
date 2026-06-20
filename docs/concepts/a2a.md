# A2A (Agent-to-Agent)

> Status: shipped (Phase 16). Tenant-scoped. **Back-compatible** — A2A is additive; a
> deployment that registers no A2A peers and declares no bridges behaves exactly as before.

**A2A** is the second agentic wire protocol Portico speaks, alongside MCP. Where MCP talks in
`tools`, `resources`, and `prompts`, A2A talks in **agent cards** and **tasks**: an agent
publishes a card describing its skills, and clients send it messages that the agent turns into
tasks. Portico is fluent in both, on the **same listener** and through the **same governance
envelope** — tenant → JWT/Virtual Key → Agent Profile → policy → audit → tracing.

Portico pins a published A2A spec revision in `internal/a2a/protocol/version.go::SpecVersion`
(currently `0.2.5`); bumping it is an RFC change, never an in-code drift. All A2A wire types live
in `internal/a2a/protocol` and nowhere else (AGENTS.md §13).

## What you can do

1. **Be an A2A endpoint.** Portico serves its own agent card at `GET /a2a/.well-known/agent.json`
   and a JSON-RPC 2.0 endpoint at `POST /a2a`, mounted under the same listener as `/mcp` and
   `/v1` and behind the same auth. The card aggregates the discovered skills of the tenant's
   registered peers.
2. **Register external A2A peers.** A peer is a tenant-scoped resource (`/api/a2a/peers`) with an
   endpoint, optional egress-auth vault reference, and a cached agent card. Peers are managed in
   the Console at **A2A → Peers**.
3. **Dispatch to peers, governed.** An inbound A2A call names its target peer
   (`params.metadata.portico_peer`); Portico enforces the caller's Agent Profile, attaches egress
   credentials from the vault, dispatches over a pooled southbound client, and audits the call —
   Portico acts as a governed single-endpoint A2A proxy.
4. **Bridge MCP → A2A.** An Agent Profile can declare that an MCP `tools/call` for a named tool
   transparently dispatches to an A2A peer task. The calling agent keeps using a tool it already
   knows; the work runs on a remote agent. (The reverse direction, A2A → MCP, is a planned
   follow-up.)
5. **Discover peer capabilities.** `POST /api/a2a/peers/{id}/refresh-card` fetches the peer's card
   from its well-known URL and caches it, so its skills appear in Portico's catalog + agent card.

## The governed envelope

Every A2A call — inbound from a client, outbound to a peer, or bridged from an MCP `tools/call` —
traverses one path:

- **Tenant + identity** from the JWT or Virtual Key (the `/a2a` routes live in the auth group).
- **Agent Profile entitlement.** `Profile.AllowsA2APeer(name)` gates which peers a consumer may
  reach; `Profile.AllowsA2ATask("peer.task")` gates specific tasks where a task is named (bridges).
  A nil/default profile allows everything (back-compat). The Agent Profile is the single source of
  consumer entitlement and routing — there is no parallel allowlist (AGENTS.md §13).
- **Egress credentials.** A peer's `egress_auth_ref` resolves from the vault to a `Bearer` on the
  outbound request. An inbound caller's `Authorization` is never forwarded — the southbound client
  builds fresh requests.
- **Audit.** Dispatches emit `a2a.dispatch`; profile rejections emit `agent_profile.violation`.

## Architecture

```
internal/a2a/
├── protocol/      # wire types — the single source of truth (SpecVersion, envelope,
│                  # methods, errors, AgentCard, Task/Message/Part/Artifact)
├── southbound/    # outbound client to peers
│   ├── http/      # JSON-RPC over HTTP (unary: message/send, tasks/get, tasks/cancel)
│   └── manager/   # per-(tenant,peer) client pool, vault-agnostic via an injected factory
├── dispatch/      # governed dispatch: profile enforcement + pool + audit
├── northbound/http/   # inbound transport: agent card + JSON-RPC, mounted at /a2a
└── ingest/        # agent-card refresh + persistence
```

Bridges live on the Agent Profile (schema + REST) and are enforced in the MCP dispatcher
(`internal/server/mcpgw`), which dispatches a bridged `tools/call` over the A2A `dispatch` seam.

## Entitlement & bridges on the Agent Profile

A profile carries two A2A allowlists and two bridge route sets:

```json
{
  "allowed_a2a_peers": ["research-agent"],
  "allowed_a2a_tasks": ["research-agent.code-review"],
  "mcp_to_a2a_bridges": [
    { "mcp_tool": "github.code-review.run", "a2a_peer": "research-agent", "a2a_task": "code-review" }
  ],
  "a2a_to_mcp_bridges": [
    { "a2a_task": "billing.refund", "mcp_tool": "billing.refund" }
  ]
}
```

Empty allowlists mean "all" (back-compat). Bridges are routing, not entitlement: a bridged call is
still gated by `AllowsA2APeer` / `AllowsA2ATask`.

## REST surface

| Method | Path                                  | Purpose                          |
|--------|---------------------------------------|----------------------------------|
| GET    | `/api/a2a/peers`                      | list peers                       |
| POST   | `/api/a2a/peers`                      | register a peer                  |
| GET    | `/api/a2a/peers/{id}`                 | peer + cached agent card         |
| PUT    | `/api/a2a/peers/{id}`                 | update a peer                    |
| DELETE | `/api/a2a/peers/{id}`                 | remove a peer                    |
| POST   | `/api/a2a/peers/{id}/refresh-card`    | fetch + cache the peer's card    |
| GET    | `/a2a/.well-known/agent.json`         | Portico's agent card             |
| POST   | `/a2a`                                | A2A JSON-RPC (governed proxy)    |

Peer + bridge management is admin-scoped and lives in the Console at **A2A → Peers** and on the
Agent Profile editor.

See also: [agent-profiles](./agent-profiles.md), [virtual-keys](./virtual-keys.md).
