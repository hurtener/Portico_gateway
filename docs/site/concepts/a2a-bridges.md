# A2A bridges & ingestion

Portico speaks both MCP and A2A on the same listener and through the same governance envelope. Bridges are the mechanism that lets those two protocol surfaces interoperate: a bridge route on an [Agent Profile](/concepts/agent-profiles) declares that an MCP `tools/call` should transparently dispatch to an A2A peer task, or that an inbound A2A task should invoke an MCP tool. Agent-card ingestion is the complementary mechanism that keeps Portico's view of peer capabilities current and surfaces them through its own catalog.

This page covers both bridge directions, the agent-card ingestion pipeline, and how discovered A2A tasks surface through the catalog.

::: info A2A overview
This page focuses on the bridge and ingestion mechanics. For the fundamentals of A2A support in Portico — what an agent card is, how the governed proxy works, and the REST surface for managing peers — see [A2A (Agent-to-Agent)](/concepts/a2a).
:::

## Bridge routes on the Agent Profile

Bridge routes are declared on the [Agent Profile](/concepts/agent-profiles). They are stored as two separate lists:

```json
{
  "mcp_to_a2a_bridges": [
    {
      "mcp_tool": "github.code-review.run",
      "a2a_peer": "research-agent",
      "a2a_task": "code-review"
    }
  ],
  "a2a_to_mcp_bridges": [
    {
      "a2a_task": "billing.refund",
      "mcp_tool": "billing.refund-handler"
    }
  ]
}
```

Each `mcp_to_a2a_bridges` entry maps a namespaced MCP tool (`"server.tool"`) to a registered peer name and the task name on that peer. Each `a2a_to_mcp_bridges` entry maps an inbound A2A task name to a namespaced MCP tool.

Bridge routes are **routing declarations, not entitlement grants**. A call dispatched through a bridge is still subject to all entitlement checks — a `mcp_to_a2a_bridges` route still requires the caller's profile to permit `AllowsA2APeer` for the named peer and `AllowsA2ATask` for the specific task. Declaring a bridge does not override the allowlists; it only specifies how the call is routed when entitlement is satisfied.

## MCP → A2A bridge

The MCP → A2A bridge allows an agent that only knows MCP tools to transparently invoke a remote A2A peer task. The calling agent issues an ordinary `tools/call` for a tool it already knows; Portico intercepts the call inside the MCP dispatcher and routes it over A2A.

### Dispatch path

When a `tools/call` arrives in the MCP gateway dispatcher:

1. **Bridge lookup.** The dispatcher calls `Profile.BridgeForMCPTool(namespacedTool)` on the resolved Agent Profile. If no bridge is declared for that tool, normal MCP routing continues.
2. **Task entitlement check.** Unlike a direct `message/send` (where the peer gate is peer-level), a bridged call names a specific task, so the dispatcher enforces `Profile.AllowsA2ATask("peer.task")` at this point. A violation produces MCP error code `ErrAgentProfileViolation` and emits an `agent_profile.violation` audit event; no outbound call is made.
3. **Argument translation.** The MCP `CallToolParams.Arguments` (a `json.RawMessage`) is mapped to an A2A `Part`. An object payload becomes a `DataPart`; a non-object or empty payload falls back to a `TextPart`. The resulting part is wrapped in a `MessageSendParams` envelope with role `user`.
4. **Governed dispatch.** The dispatcher calls `SendMessageByPeerName` on the A2A dispatch layer. That layer resolves the peer by name (tenant-scoped), enforces `AllowsA2APeer`, attaches egress credentials from the vault, and issues the `message/send` over the pooled southbound client.
5. **Result translation.** The raw A2A result (a `Task` or `Message`) is carried back as an MCP `CallToolResult`. The JSON is placed in a `text` content block so MCP clients that do not consume `structuredContent` can still read it; it is also placed in `structuredContent` for clients that do.

The call-metadata on the outbound `message/send` includes `a2a_task` and `bridged_from_mcp_tool`, so the receiving peer can identify bridged calls. An `a2a.dispatch` audit event is emitted on success; a `tool_call.failed` event is emitted on error.

### Wire example

```jsonc
// Inbound MCP tools/call
{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "tools/call",
  "params": {
    "name": "github.code-review.run",
    "arguments": { "pr_url": "https://github.com/example/repo/pull/42" }
  }
}

// Outbound A2A message/send (constructed by the bridge)
{
  "jsonrpc": "2.0",
  "id": "mcp-bridge-1",
  "method": "message/send",
  "params": {
    "message": {
      "role": "user",
      "messageId": "mcp-bridge-1",
      "kind": "message",
      "parts": [
        {
          "kind": "data",
          "data": { "pr_url": "https://github.com/example/repo/pull/42" }
        }
      ]
    },
    "metadata": {
      "a2a_task": "code-review",
      "bridged_from_mcp_tool": "github.code-review.run"
    }
  }
}
```

The `params.metadata.portico_peer` field is **not** set on bridged calls — it is used only for direct governed proxy calls. The bridge resolves the peer by the name declared in the bridge route.

### Error codes

| Code | Meaning |
|------|---------|
| `-32010` (`ErrProfileViolation`) | Agent Profile does not permit the bridged peer or task |
| `-32004` (`ErrUnsupportedOperation`) | Target peer is registered but disabled |
| `-32603` (`ErrInternalError`) | Upstream unavailable or result serialization failure |

These are A2A protocol error codes carried in the JSON-RPC response. The MCP response to the original `tools/call` echoes them with the appropriate MCP error wrapper so the calling MCP client receives a typed error regardless of which layer produced it.

## A2A → MCP bridge

The A2A → MCP bridge is the inverse: an inbound A2A `message/send` that names a bridged task is dispatched to a registered MCP tool instead of to an external peer. The external caller issues a standard `message/send`; Portico consults the calling Agent Profile's `a2a_to_mcp_bridges` list, routes the call internally to the named MCP tool, and translates the MCP result back to an A2A `Task` response.

The bridge lookup uses `Profile.BridgeForA2ATask(taskName)`. When a bridge is found, the first `DataPart` of the inbound message is unpacked as the tool's arguments; the MCP `CallToolResult` is translated to an A2A `Task` in state `completed` with its result as an artifact.

::: warning In progress
The A2A → MCP bridge dispatch path is a planned addition (Phase 16 acceptance #8). The schema — `A2AToMCPBridge` struct on the Agent Profile, the `BridgeForA2ATask` lookup method, the REST surface for managing bridge routes — is complete and shipped. The northbound handler routing that intercepts an inbound task name and invokes an MCP tool is the remaining unit.
:::

## Agent-card ingestion

An A2A peer's agent card is its discovery document: a JSON object served at `GET /.well-known/agent.json` that advertises the peer's identity, protocol version, capabilities, and the skills (task groups) it offers.

Portico caches each peer's card as `agent_card_json` on the peer row, so the catalog and Portico's own outbound agent card can surface the peer's skills without a live round-trip on every call.

### Fetching a card on demand

```http
POST /api/a2a/peers/{id}/refresh-card
Authorization: Bearer <token>
```

This triggers the `ingest.Refresher`. It:

1. Loads the peer row (tenant-scoped) to confirm the peer exists.
2. Acquires a governed southbound client for the peer via the managed pool (including vault-resolved egress credentials if the peer has an `egress_auth_ref`).
3. Derives the card URL from the peer's endpoint by replacing any path with `/.well-known/agent.json` and stripping query and fragment:

```
https://research-agent.internal:9000/rpc
  → https://research-agent.internal:9000/.well-known/agent.json
```

   A non-URL endpoint string has `/.well-known/agent.json` appended as a suffix.
4. Issues a plain HTTP GET (not a JSON-RPC call — agent-card discovery is a standard GET).
5. Marshals the decoded card to JSON and persists it on the peer row via `PutPeer`.

The refreshed peer (including the new `agent_card_json`) is returned in the response body. On fetch or decode failure, the endpoint returns HTTP 502 and the storage row is unchanged.

### Card aggregation

Portico serves its own agent card at `GET /a2a/.well-known/agent.json`. The card is assembled at request time and aggregates the skills from the tenant's registered peers whose cards have been refreshed. A peer with an empty `agent_card_json` contributes no skills; a peer with a populated card contributes all `skills` entries from its cached card.

This means an external A2A client that discovers Portico sees a unified skill surface across all the tenant's registered peers, without knowing that those peers are separate endpoints. The aggregated card carries Portico's own identity, version, and capabilities; the `skills` array is the union of the peer cards' skills.

### Scheduled refresh and catalog drift

Portico is designed to run a background poller that periodically calls the refresher for each enabled peer and surfaces detected capability changes:

- Changes in a peer's card emit a `catalog.drift` event.
- Discovered A2A tasks (drawn from the card's `skills` array) appear in the catalog as items with `kind: a2a_task`, alongside MCP tools with `kind: tool`.

::: warning In progress
The scheduled refresh poller and catalog `kind:a2a_task` integration are planned as Phase 16 acceptance #9. Manual `refresh-card` and card aggregation in Portico's own agent card are complete and shipped.
:::

## Governance envelope

Every call that crosses a bridge, every card fetch, and every A2A dispatch traverses the same governance envelope:

1. **Tenant + identity.** All `/a2a` routes and the `/api/a2a` management routes are mounted inside the auth middleware group. The tenant identity and the resolved Agent Profile are in the request context before any bridge logic runs.
2. **Agent Profile entitlement.** `AllowsA2APeer` gates which peers a consumer can reach. `AllowsA2ATask("peer.task")` gates specific tasks (applied when a task name is explicit, as in a bridge call). A nil or default profile allows everything (back-compatible).
3. **Egress credentials.** A peer's `egress_auth_ref` is resolved from the vault to a `Bearer` header on every outbound request. Inbound caller credentials are never forwarded.
4. **Audit.** Successful dispatches emit `a2a.dispatch`. Profile violations emit `agent_profile.violation`. Bridge-side events on the MCP half emit `a2a.dispatch` (success) or `tool_call.failed` (error).

See [Credentials vault](/concepts/credentials-vault) for egress auth configuration and [Audit](/concepts/audit) for event shapes.

## Console: A2A → Peers

The Console exposes a dedicated **A2A → Peers** section under the tenant workspace. From there, an operator can:

- Register, update, and remove A2A peers (name, endpoint, egress auth vault reference, enabled state).
- Trigger a card refresh for any peer and inspect the cached card content.
- View the peer's discovered skills once its card has been fetched.

Bridge routes (`mcp_to_a2a_bridges` and `a2a_to_mcp_bridges`) are managed on the [Agent Profile](/concepts/agent-profiles) editor, which exposes both bridge lists as editable sections alongside the standard allowlists.

## Peer registration quick-start

```yaml
# Register a peer via the REST API
POST /api/a2a/peers
Content-Type: application/json

{
  "name": "research-agent",
  "endpoint": "https://research-agent.internal:9000/rpc",
  "egress_auth_ref": "research-agent-bearer",
  "enabled": true
}
```

Then fetch its card:

```bash
curl -X POST https://portico.example.com/api/a2a/peers/{id}/refresh-card \
  -H "Authorization: Bearer $TOKEN"
```

And add an MCP → A2A bridge to the caller's Agent Profile:

```json
{
  "mcp_to_a2a_bridges": [
    {
      "mcp_tool": "research.search",
      "a2a_peer": "research-agent",
      "a2a_task": "semantic-search"
    }
  ],
  "allowed_a2a_peers": ["research-agent"],
  "allowed_a2a_tasks": ["research-agent.semantic-search"]
}
```

With this configuration, any `tools/call` for `research.search` from a session bound to this profile is silently routed to the `research-agent` peer's `semantic-search` task. The calling agent requires no changes.

## Related

- [A2A (Agent-to-Agent)](/concepts/a2a) — protocol fundamentals, peer management REST surface, and the governed proxy model
- [Agent Profiles](/concepts/agent-profiles) — the full Agent Profile schema, allowlists, bridge declarations, and JWT binding
- [Drift detection](/concepts/drift-detection) — how catalog changes (including A2A card changes) are detected and surfaced
- [Credentials vault](/concepts/credentials-vault) — egress auth resolution and `egress_auth_ref` configuration
- [Audit](/concepts/audit) — `a2a.dispatch` and `agent_profile.violation` event shapes
- [Catalog and sessions](/concepts/catalog-and-sessions) — catalog kinds including `a2a_task` entries from discovered peer skills
