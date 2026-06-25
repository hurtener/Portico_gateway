# Code Mode

Code Mode is an alternative tool-presentation mode for MCP clients. Instead of exposing the full
namespaced tool catalog, a Code Mode session presents **four meta-tools** and lets the model
orchestrate multi-tool work by writing small Starlark snippets. Intermediate results stay inside
the sandbox; only the final `result` value crosses back into the conversation.

The key consequence: a catalog of 150 tools never lands in the model's context window.
The model loads only the stubs it chooses to inspect, issues a single `executeToolCode` request
that internally calls several tools in sequence, and the session receives one structured result
instead of N round-trip exchanges.

Code Mode is not a parallel execution path. It is a different *projection* of the same catalog
under the **same governance**: every tool call a snippet makes traverses the identical
tenant → JWT scope → policy → approval → vault → audit → telemetry envelope as a direct
`tools/call`. See [Token-savings estimator](/concepts/code-mode-savings) for measurements.

::: info Opt-in only
Code Mode activates only when the MCP client requests it at `initialize` time. Existing clients
that do not send the capability flag see the regular namespaced catalog — no behavioral drift.
:::

---

## How it works

A Code Mode session follows this flow:

```
1. Client sends initialize with code_mode capability
   → Portico advertises four mcp.* meta-tools instead of the catalog

2. Model calls mcp.listToolFiles
   → receives a list of virtual .pyi paths (one per server by default)

3. Model calls mcp.readToolFile on a path it cares about
   → receives compact Python stub signatures for that server's tools

4. Model writes a Starlark snippet and calls mcp.executeToolCode
   → Portico runs the snippet in a hardened sandbox
   → each tool call inside the snippet traverses the full governed dispatch path
   → final `result` value is returned, plus telemetry (tool_calls, tokens_saved_est, duration_ms)
```

The catalog snapshot the session was opened against is immutable for the execution's lifetime.
Stubs are a deterministic projection of that snapshot: the same snapshot always produces
byte-identical `.pyi` files.

---

## Opting in

At `initialize` time, include the following capability in the request:

```json
{
  "capabilities": {
    "experimental": {
      "portico": {
        "code_mode": {
          "enabled": true,
          "binding_level": "server",
          "max_tool_calls": 20
        }
      }
    }
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | — | Required. Activates Code Mode for this session. |
| `binding_level` | string | `"server"` | `"server"` renders one stub file per server; `"tool"` additionally renders one file per individual tool. |
| `max_tool_calls` | int | `20` | Per-execution tool-call budget. Operators can lower it further via policy. |

Sessions that omit the capability receive the regular namespaced tool catalog unchanged.

---

## The four meta-tools

When Code Mode is active, `tools/list` returns exactly these four tools. No catalog tools appear.

| Meta-tool | Purpose |
|---|---|
| `mcp.listToolFiles` | Enumerate the virtual `.pyi` stub file system for the session's snapshot |
| `mcp.readToolFile` | Read one stub file — compact function signatures for a server or individual tool |
| `mcp.getToolDocs` | Full docs, JSON Schema, risk class, and approval policy for named tools |
| `mcp.executeToolCode` | Run a Starlark snippet that calls tools and returns a final `result` |

The `mcp.*` namespace is reserved for these meta-tools. No other code path may register tools
under that prefix.

### Discovery flow example

```
mcp.listToolFiles  → ["index.md", "servers/github.pyi", "servers/jira.pyi", ...]
mcp.readToolFile   → "def list_issues(repo: str, state: str = None) -> dict: ..."
mcp.getToolDocs    → {"docs": [{"name": "github.list_issues", "risk_class": "read", ...}]}
mcp.executeToolCode→ {"result": ..., "tool_calls": 3, "tokens_saved_est": 1280, "duration_ms": 712}
```

---

## The virtual catalog

The catalog projector converts a session snapshot into a virtual file system.

**At `"server"` binding level (default):**

```
index.md
servers/github.pyi
servers/jira.pyi
servers/slack.pyi
```

Each `.pyi` file lists every tool the server exposes as a Python function signature with type
annotations and a docstring from the tool description:

```python
# Server "github" exposed as Starlark module "github"
# 12 tool(s). Call as: github.<function>(...)

def list_issues(repo: str, state: str = None) -> dict:
    """List open or closed issues for a repository."""
    ...

def create_issue(repo: str, title: str, body: str = None, labels: list[str] = None) -> dict:
    """Create a new issue."""
    ...
```

**At `"tool"` binding level**, each individual tool also gets its own file:

```
servers/github/list_issues.pyi
servers/github/create_issue.pyi
```

### JSON Schema to Python type mapping

The projector translates each tool's JSON Schema input parameters to Python annotations:

| JSON Schema type | Python annotation |
|---|---|
| `string` | `str` |
| `integer` | `int` |
| `number` | `float` |
| `boolean` | `bool` |
| `object` | `dict` |
| `array` | `list` or `list[T]` when items type is known |
| `["string", "null"]` | `str` (first non-null type) |
| unknown / missing type | `Any` |

Required parameters appear first in the signature; optional parameters follow with `= None`.

---

## Writing Starlark snippets

Snippets are [Starlark](https://github.com/bazelbuild/starlark) — a deliberately small,
deterministic Python subset. The snippet assigns its output to the global `result`:

```python
issues = github.list_issues(repo="owner/repo", state="open")
recent = [i for i in issues["items"] if i["age_days"] < 7]
result = {"count": len(recent), "titles": [i["title"] for i in recent]}
```

Each tool call returns the raw MCP `CallToolResult` content, decoded into Starlark data types
(dicts, lists, strings, integers, floats, bools). Tool arguments must be keyword arguments;
positional arguments are rejected.

**Available built-ins** (exact allowlist from the implementation):

```
print  len    range  enumerate  zip     sorted
min    max    sum    dict       list    str
int    float  bool   any        all     repr   type
```

**Standard library modules available in snippets:**

| Module | Available surface |
|---|---|
| `json` | `json.encode(v)`, `json.decode(s)` |
| `math` | Full pure-math module |
| `time` | `time.now()` — returns execution timestamp, frozen per run, coarsened to the second |

Everything else is absent. There is no `import`, no `load`, no file I/O, no network, no
subprocess access.

---

## Sandbox hardening

The runtime treats every snippet as hostile input. The threat model covers prompt injection,
namespace hijacking, resource exhaustion, and governance bypass. The key defenses are:

### Static safety gate

Before execution begins:

1. **`load` statements are rejected at parse time.** Any snippet containing a `load(...)` statement
   returns `code_mode.unsafe_call` immediately, before the Starlark interpreter runs.
2. **Allowlist gate.** After compilation, the resolver checks that every free identifier in the
   snippet resolves to a name the sandbox explicitly bound — either an allowlisted built-in,
   a stdlib module (`json`, `math`, `time`), or a tool module from the snapshot. Any name that
   resolves to Starlark's Universe scope (a real Starlark built-in not on the allowlist —
   such as `getattr`, `set`, `hasattr`, `dir`, `hash`) is rejected with `code_mode.unsafe_call`.
3. **`thread.Load` is nil.** Even if a `load` statement somehow reached execution, the Starlark
   thread has no load callback. The static rejection provides a precise error message; the nil
   callback is defense in depth.

### Execution budgets

Every execution is bounded on five independent dimensions. Exceeding any of them terminates the
execution with `code_mode.budget_exceeded` and names the dimension in `error.data.detail`.

| Dimension | Default | Detail value |
|---|---|---|
| Starlark abstract steps | 100,000 | `steps` |
| Wall-clock time | 30 seconds | `wall_clock` |
| `print()` output | 1 MiB | `output_bytes` |
| Tool calls | 20 | `tool_calls` |
| Heap growth | 256 MiB | `memory` |

A misconfigured zero value for any budget is replaced by the conservative default — a zero budget
can never mean "unlimited."

::: warning Memory budget is a backstop
The heap-growth budget is enforced by a sampling watchdog that reads process-level heap statistics
every 20 milliseconds. Under concurrent executions a sibling's allocation can inflate the sample.
The budget stops gradual and looping allocation bombs but does not provide precise per-execution
isolation. A single catastrophic Starlark allocation (larger than one watchdog interval) is a
documented residual. For strict per-execution memory isolation, run Portico under a memory cgroup.
:::

### No governance bypass

There is exactly one path from snippet code to a tool call: the `ToolDispatcher` seam at
`runtime.ToolDispatcher.DispatchToolCall`. The production implementation of this interface is the
same internal function that handles a direct `tools/call` MCP request. There is no second path.
This seam is what guarantees in-sandbox calls produce identical audit events, policy enforcement,
and credential injection as direct calls.

---

## Approval suspend and resume

When a tool called from inside a snippet requires operator approval, the execution cannot proceed
synchronously. Portico suspends the execution using a deterministic replay strategy:

1. The runtime detects `approval_required` from the dispatcher.
2. It records a continuation: the original snippet code, the frozen execution clock, the results of
   all tool calls that completed before the suspend (indexed by call ordinal), and the buffered
   `print()` output up to the suspend point.
3. `executeToolCode` returns with `status: "approval_required"`, the `approval_id`, and a
   `continuation_token`. If the client supports MCP elicitation, the elicitation is also emitted.
4. The model surfaces the approval request. The operator approves via the Console or the REST API.
5. On resume, the runtime re-runs the identical snippet: calls with ordinals below the suspend
   point are served from the cached results without re-dispatching (so prior writes are never
   replayed), and the awaited call is re-dispatched live with the granted approval ID threaded
   into the context.

**Determinism requirements for replay:**
- The session snapshot must not have rotated between suspension and resume. If it has,
  the runtime fails with `code_mode.snapshot_drifted`.
- `time.now()` returns the original frozen timestamp on replay, not the current wall clock.
- Starlark is deterministic given identical global bindings.

Continuations have a configurable TTL (default 24 hours). After expiry, resume returns
`code_mode.continuation_expired`. A background sweeper removes expired rows.

---

## Operator policy

The `Policy` type governs the `executeToolCode` meta-tool at the execution level, distinct from
the per-tool policy that governs each individual tool call inside the snippet.

| Policy field | Description |
|---|---|
| `Disabled` | Kill switch. Rejects all `executeToolCode` calls with `code_mode.disabled_by_policy`. |
| `MaxExecutionBytes` | Maximum snippet source size in bytes. 0 means no limit. |
| `MaxToolCallsInside` | Acts as a ceiling on `max_tool_calls`. A session can request fewer but not more. |
| `AllowedBindingLevels` | Restricts which binding levels may run (e.g. `["server"]` forbids `"tool"` level). |
| `RequireApprovalOnExecute` | Gates every `executeToolCode` call behind the approval flow before the snippet runs. |
| `DenyUnsafeStarlark` | Escalates a static-gate rejection to an audited policy denial. |

::: tip Default posture
The zero-value policy is fully permissive within tenant constraints. Operators tighten it
explicitly. This mirrors the broader [policy model](/concepts/policy) — deny is a deliberate
operator action, not the default.
:::

Continuation resumes are not re-evaluated against policy. The original execution already passed
the gate; the continuation is single-use, tenant-scoped, and TTL-bounded.

---

## Error reference

`executeToolCode` returns typed JSON-RPC errors. The `error.data.code` field names the
precise failure class; `error.data.detail` provides specifics.

| `error.data.code` | When it fires | `error.data.detail` |
|---|---|---|
| `code_mode.unsafe_call` | Static gate rejected a `load` statement or a non-allowlisted identifier | The offending identifier |
| `code_mode.budget_exceeded` | An execution budget tripped | `steps`, `wall_clock`, `tool_calls`, `output_bytes`, or `memory` |
| `code_mode.compile_error` | The snippet failed to parse or compile | Starlark syntax error message |
| `code_mode.runtime_error` | Starlark runtime error (type error, divide-by-zero, etc.) | Error description |
| `code_mode.tool_error` | A governed tool call inside the snippet failed | Namespaced tool name |
| `code_mode.approval_required` | An in-sandbox tool call requires approval; execution suspended | Namespaced tool name |
| `code_mode.execution_too_large` | Snippet exceeds `Policy.MaxExecutionBytes` | — |
| `code_mode.binding_level_denied` | Session's binding level is not in `Policy.AllowedBindingLevels` | — |
| `code_mode.disabled_by_policy` | `Policy.Disabled` is set | — |
| `code_mode.snapshot_drifted` | Snapshot rotated between suspension and resume | — |
| `code_mode.continuation_expired` | Continuation TTL elapsed before resume | — |

---

## Token savings

Each `executeToolCode` response includes a `tokens_saved_est` field. It is a deterministic
estimate of the tokens Code Mode saved relative to a hypothetical plain-mode session over the same
snapshot:

```
saved = (catalog_render_tokens - meta_tools_render_tokens)
      + (num_tool_calls × per_call_overhead_tokens)
      - (code_bytes + result_bytes) / chars_per_token
```

Where:
- `catalog_render_tokens` — estimated token cost of rendering the full tool catalog as an
  OpenAI-format tools blob (4 chars per token approximation, plus 40-character per-tool framing).
- `meta_tools_render_tokens` — constant cost of the four meta-tool definitions (~225 tokens).
- `per_call_overhead_tokens` — ~80 tokens per collapsed round trip (measured from a replay corpus).
- The code and result bytes are the cost Code Mode *adds*.

The estimate is clamped to zero when the snippet is large enough to cost more than the catalog it
replaced. For large catalogs with multiple tool calls, savings typically land in the hundreds to
low thousands of tokens per execution. See [Token-savings estimator](/concepts/code-mode-savings)
for the full formula and per-snapshot breakdown.

---

## Audit and telemetry

Every Code Mode execution produces a complete audit and span tree:

**Audit events emitted per execution:**
- `code_mode.execution_started` — snippet SHA, session, tenant, binding level
- `code_mode.execution_completed` — tool call count, estimated tokens saved, duration
- `code_mode.execution_failed` — error code and budget dimension (never snippet content or tool arguments)

**Audit events emitted per in-sandbox tool call:**
- Standard `tool_call.start` and `tool_call.complete` events, identical to those from a direct `tools/call`.
  The `code_mode.execution_id` is attached as a parent attribute.

**Spans:**
- One parent `code_mode.execution` span per run.
- One child `mcp.tool_call` span per in-sandbox tool dispatch, parented under the execution span.

Snippet content, raw tool arguments, and raw results are never included in audit events.
The final `result` and `print()` output pass through the same audit redactor used for direct
tool calls before they appear in the response or audit records. The redactor scrubs known secret
shapes and sensitive key patterns; credentials never enter the sandbox because the vault injects
them into downstream requests at the dispatcher layer, not before.

---

## Skill Pack snippets

[Skill Packs](/concepts/skill-packs) can declare canonical Starlark snippets in their manifest
under an optional `code_mode:` block. These snippets are indexed by the skills runtime and
surfaced in the Console's skill detail view and the Playground's Code Mode editor as one-click
inserts. Skill-provided snippets carry the same trust level as the Skill itself; their execution
history is queryable from the skill's audit view.

---

## CLI tools

Two subcommands are available for debugging and operator use:

```bash
# Dump the projected stub files for a snapshot (no execution)
portico code-mode render --tenant <tenant-id> --session <session-id>

# Execute a Starlark snippet against a live session (requires admin scope; emits an audit event)
portico code-mode exec --tenant <tenant-id> --session <session-id> --code @snippet.star
```

---

## Related

- [Token-savings estimator](/concepts/code-mode-savings) — the estimation formula and per-execution breakdown
- [Approvals](/concepts/approvals) — how approval-required tools interact with execution continuations
- [MCP Gateway](/concepts/mcp-gateway) — the overall MCP request lifecycle Code Mode sits within
- [Skill Packs](/concepts/skill-packs) — shipping canonical Starlark snippets alongside a Skill
- [Use Code Mode](/guides/use-code-mode) — step-by-step guide for enabling and testing Code Mode in a session
- [Policy](/concepts/policy) — the operator-configurable policy engine that governs both the meta-tool and every in-sandbox tool call
