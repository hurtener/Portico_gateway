# Approvals

Portico's approval system is a hard gate in the tool-call path. When a tool is flagged `requires_approval`, every dispatch — regardless of caller, session, or agent — pauses at that gate and waits for a human decision before proceeding. There are no bypass mechanisms, no opt-outs, and no silent promotions: the gate is binary and the audit trail is complete.

## The headless model

Portico does not render its own approval UI. This is intentional and non-negotiable: the Console shows pending approvals read-only; it never presents an interactive decision surface. Instead, Portico emits the decision request over the MCP channel itself using `elicitation/create` — a server-initiated request that the client (the AI host, IDE, or agent harness) renders for the operator.

This design has a direct consequence for operators: the approval experience lives in the tool that connects to Portico, not in a web portal. The Console is a monitoring surface. The operator resolves approvals in the client, or via the REST API if the client doesn't support elicitation.

## When an approval triggers

The policy engine (`internal/policy/engine.go`) computes a `Decision` for every `tools/call`. The decision includes a `RequiresApproval` flag, which is set when any of the following conditions hold:

1. **Risk-class default.** The tool's effective risk class is one that mandates approval by default:

   | Risk class | Approval by default |
   |---|---|
   | `read` | No |
   | `write` | No (policy-configurable) |
   | `sensitive_read` | **Yes** |
   | `external_side_effect` | **Yes** |
   | `destructive` | **Yes** |

   The effective risk class is resolved in priority order: Skill Pack override → server `auth.default_risk_class` → engine default (fallback to `write`).

2. **Explicit Skill Pack declaration.** A Skill Pack can list any tool under `binding.policy.requires_approval`, triggering approval regardless of the tool's risk class.

3. **`destructive` risk class is unconditional.** A tool carrying `destructive` always requires approval. This class cannot be overridden to off — the code path in `requiresApprovalDefault` hard-codes it.

::: warning Approval bypass is not a feature
A tool flagged `requires_approval` always goes through the approval flow. There is no configuration key, scope, or API call that bypasses it. Attempts to add such a bypass are treated as security bugs.
:::

## Two paths: elicitation and fallback

The approval flow (`internal/policy/approval/flow.go`) selects one of two execution paths depending on the client's advertised capabilities.

### Path 1 — elicitation (preferred)

When the session declares `elicitation` support in its capability negotiation, Portico sends a `elicitation/create` server-initiated request over the session's SSE channel. The northbound transport (`internal/mcp/northbound/http/server_initiated.go`) writes it as an `event: server_request` SSE event; the client's POST handler matches the response by correlation ID when the operator responds.

The request carries a structured schema so the client can render a form:

```json
{
  "jsonrpc": "2.0",
  "id": "s_aB3Cde...",
  "method": "elicitation/create",
  "params": {
    "message": "Approve calling github.create_review_comment? Risk: external_side_effect.",
    "requestedSchema": {
      "type": "object",
      "properties": {
        "approve": { "type": "boolean", "title": "Approve this action?" },
        "note":    { "type": "string",  "title": "Optional reason" }
      },
      "required": ["approve"]
    },
    "_meta": {
      "portico": {
        "approval_id":   "apr_1720000000000000000_1",
        "tool":          "github.create_review_comment",
        "risk_class":    "external_side_effect",
        "skill_id":      "github.code-review",
        "args_summary":  "{\"owner\":\"acme\",\"repo\":\"web\",\"pr\":42,\"body\":\"…\"}",
        "expires_at":    "2026-06-25T14:35:00Z"
      }
    }
  }
}
```

The gateway blocks on the elicitation until the client responds or the timeout expires. Possible client responses:

- **`accept` with `{"approve": true}`** — tool call proceeds; the approval row is marked `approved`.
- **`accept` with `{"approve": false}`**, **`reject`**, or **`cancel`** — the call returns `policy_denied` with `reason: user_denied`; the approval row is marked `denied`.
- **Timeout or SSE disconnect** — the call returns `approval_timeout`; the row is marked `expired`.

Per-session serialization is enforced: if multiple tool calls are awaiting approval for the same session simultaneously, the `ServerInitiatedRequester` serializes the elicitations behind a per-session mutex so they cannot interleave.

### Path 2 — fallback structured error

When the client does not advertise elicitation support, Portico cannot block on an interactive prompt. Instead, the dispatcher returns a JSON-RPC error immediately:

```jsonc
{
  "jsonrpc": "2.0",
  "id": 7,
  "error": {
    "code": -32001,
    "message": "approval_required",
    "data": {
      "tool":                "github.create_review_comment",
      "risk_class":          "external_side_effect",
      "skill_id":            "github.code-review",
      "approval_id":         "apr_1720000000000000000_1",
      "approval_status_url": "https://gateway.example.com/v1/approvals/apr_...",
      "expires_at":          "2026-06-25T14:35:00Z",
      "args_summary":        { "owner": "acme", "repo": "web", "pr": 42 }
    }
  }
}
```

The pending approval row is written to storage before the error is returned. An operator can act via the REST API:

```http
POST /v1/approvals/{id}/approve
Content-Type: application/json

{"note": "Reviewed — looks correct"}
```

```http
POST /v1/approvals/{id}/deny
```

Only requests carrying the `admin` scope can approve or deny via the REST API. The decision is recorded in the audit log with the acting user's ID and `channel: manual`.

::: info Pending rows stay pending until acted on
The fallback error returns immediately, but the approval row persists. The agent can surface the `approval_status_url` to the operator, or the operator can poll `GET /v1/approvals?status=pending`. Once approved, the agent retries the original call within the replay window — see below.
:::

## Approval storage and lifecycle

Each approval is a row in the `approvals` table. Status transitions are one-way:

```
pending → approved
pending → denied
pending → expired
```

Fields persisted with each row:

| Field | Description |
|---|---|
| `id` | Gateway-generated identifier (prefixed `apr_`) |
| `tenant_id` | Owning tenant — all approval reads and writes are tenant-scoped |
| `session_id` | MCP session that triggered the call |
| `user_id` | User identity from the JWT |
| `tool` | Fully-qualified namespaced tool name (e.g. `github.create_review_comment`) |
| `args_summary` | First 1 024 bytes of the JSON arguments (display only) |
| `risk_class` | Resolved risk class at time of evaluation |
| `status` | `pending` / `approved` / `denied` / `expired` |
| `expires_at` | `created_at + approval_timeout` (per-tenant or engine default of 5 minutes) |
| `metadata.args_hash` | SHA-256 of the full raw argument bytes — used for replay-window identity |
| `metadata.skill_id` | Skill that flagged the requirement |

A background sweeper (`Flow.Sweep`) runs once a minute and marks rows whose `expires_at` has passed as `expired`.

## Replay window

When an operator approves a pending approval via the REST API and the agent retries the same tool call, Portico needs to recognise the prior decision rather than opening a new approval. It does this by threading the `approval_id` into the retried `tools/call` via the `CallContext.ApprovalID` field.

On retry, `Flow.replayDecision` looks up the row and verifies a **strict three-way identity**:

1. Same `tool` name.
2. Same `skill_id` stored in metadata.
3. Same `args_hash` — a SHA-256 digest of the **exact raw argument bytes** of the retry call, compared against the hash stored at approval time.

All three must match. Any mismatch fails closed: Portico opens a fresh pending row rather than granting the prior decision. The hash is byte-exact (not JSON-canonicalized) so that numerically distinct large integers and duplicate-key payloads cannot produce a hash collision that would allow an approval to replay onto different arguments.

::: warning Replay is scoped strictly
Approval caching applies only within the documented replay window, with the same arguments and the same skill ID. An approval for tool A cannot replay onto tool B. An approval granted for one set of arguments cannot replay onto a different set of arguments, even if the display summary looks similar.
:::

## Audit events

Every approval lifecycle step produces an audit event. All events carry `tenant_id`, `session_id`, and `user_id`.

| Event type | When |
|---|---|
| `approval.pending` | Approval row inserted, before elicitation or fallback |
| `approval.decided` | Operator or client made a decision (`approved` or `denied`) |
| `approval.expired` | Row passed `expires_at` without a decision |
| `approval.replayed` | A prior approved decision was recognised in the replay window |

Audit events are passed through the redactor before persistence — tool argument payloads are summarized and truncated; bearer tokens and other recognizable secret patterns are stripped. See [Audit](/concepts/audit).

## Code Mode and approval suspension

[Code Mode](/concepts/code-mode) runs agent-authored Starlark snippets inside a sandboxed runtime. Tool calls issued from within the sandbox traverse the same governed dispatch path as direct MCP `tools/call` requests — the same policy engine, the same approval flow, the same audit trail.

When a sandbox call hits a `requires_approval` tool, the runtime cannot simply block: the Starlark thread has no I/O loop. Instead, it uses a **continuation-based suspension** pattern:

1. The dispatcher returns `approval_required` to the runtime.
2. The runtime serializes the Starlark execution frame — including the current code position, all local variable bindings, and cached results from prior tool calls in the same execution — into a `code_mode_continuations` row.
3. The `executeToolCode` response returns to the caller with `status: "approval_required"`, an `approval_id`, and a `continuation_token`.
4. The model surfaces the approval request to the operator (or the host receives the elicitation if the client supports it).
5. When the operator approves and the agent re-invokes `executeToolCode` with `continuation_token=...`, the runtime reloads the continuation row, re-executes the snippet deterministically up to the suspended call site using cached results for prior calls, substitutes the newly-approved result, and continues from that point.

The Starlark state never leaves Portico. The agent receives only a structured token that references the persisted continuation; it cannot inspect or modify the suspended frame. Continuations expire after 24 hours (configurable); a background sweeper removes expired rows with a `code_mode.continuation_expired` error on any subsequent resume attempt.

## Configuring approval timeouts

The default approval timeout is 5 minutes. Override it per tenant in the configuration:

```yaml
tenants:
  - id: acme
    policy:
      approval_timeout: 10m
```

The timeout applies to both the elicitation path (the client must respond before the duration elapses) and the pending-row lifetime on the fallback path (after which the row moves to `expired`).

## Console read-only view

The Console's Approvals page lists pending, approved, denied, and expired approval rows for the authenticated tenant. Operators with `admin` scope can approve or deny pending rows directly from the Console UI — this calls the same `POST /v1/approvals/{id}/approve` and `POST /v1/approvals/{id}/deny` REST endpoints described above.

The Console never renders an interactive elicitation prompt. That surface belongs to the AI host.

## REST API reference

```
GET  /v1/approvals                  # list; filter by ?status=pending&since=...
GET  /v1/approvals/{id}             # single approval
POST /v1/approvals/{id}/approve     # admin scope required
POST /v1/approvals/{id}/deny        # admin scope required
```

Response shape for a single approval:

```json
{
  "id": "apr_1720000000000000000_1",
  "tenant_id": "acme",
  "session_id": "sess_...",
  "user_id": "u_...",
  "tool": "github.create_review_comment",
  "args_summary": "{\"owner\":\"acme\",\"repo\":\"web\",\"pr\":42,...}",
  "risk_class": "external_side_effect",
  "status": "pending",
  "created_at": "2026-06-25T14:30:00Z",
  "expires_at": "2026-06-25T14:35:00Z",
  "decided_at": null
}
```

## Related

- [Policy](/concepts/policy) — rule evaluation order, allow/deny lists, and risk class assignment.
- [MCP Northbound](/concepts/mcp-northbound) — the SSE transport and server-initiated request protocol that carries `elicitation/create`.
- [Code Mode](/concepts/code-mode) — how the sandbox runtime integrates with the approval flow via continuation-based suspension.
- [Audit](/concepts/audit) — the full event taxonomy and query API.
- [Skill Packs](/concepts/skill-packs) — how Skill Packs declare `requires_approval` and per-tool `risk_classes`.
- [Security Model](/concepts/security-model) — the non-negotiable rules that govern approvals, bypass, and credential handling.
