# Code Mode

> Status: shipping (Phase 13.5). Per-session, per-tenant, **opt-in**. Existing clients are
> unaffected.

Code Mode is an alternative tool-presentation mode for MCP clients. Instead of seeing the
full namespaced tool catalog, a Code Mode session sees **four meta-tools** and orchestrates
work by writing small **Starlark** snippets that call tools through bindings. Intermediate
results stay inside the sandbox; only the final `result` value crosses back into the
conversation.

This cuts the token spend of multi-tool sessions: a 150-tool catalog never lands in the
model's context window — only the four meta-tool signatures plus the stubs the model
*chose* to load — and N per-call round trips collapse into one larger round trip that calls
tools in a loop.

Code Mode is not a parallel universe. It is a different *projection* of the same catalog
with the **same governance**: every tool call a snippet makes goes through the identical
tenant → JWT scope → policy → approval → vault → audit → telemetry envelope as a direct
`tools/call`.

## Opting in

A client requests Code Mode at `initialize` time via the experimental capability:

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

- `enabled` (required) — turn Code Mode on for this session.
- `binding_level` — `"server"` (default) renders one stub file per server; `"tool"` also
  renders one per tool.
- `max_tool_calls` — per-execution tool-call budget (default 20).

A session that does not send this capability sees the regular namespaced catalog — no
behavioural drift.

## The four meta-tools

| Meta-tool             | Purpose                                                                |
|-----------------------|------------------------------------------------------------------------|
| `mcp.listToolFiles`   | enumerate the virtual `.pyi` stub file system for the session snapshot  |
| `mcp.readToolFile`    | read one stub file (compact function signatures for a server or tool)   |
| `mcp.getToolDocs`     | full docs / JSON Schema / risk class / approval policy for named tools  |
| `mcp.executeToolCode` | run a Starlark snippet that calls tools and returns a final `result`     |

### Discovery flow

```
listToolFiles  → ["index.md", "servers/github.pyi", "servers/jira.pyi", ...]
readToolFile   → "def list_issues(repo: str, state: str = None) -> dict: ..."
getToolDocs    → {"docs": [{"name": "github.list_issues", "risk_class": "read", ...}]}
executeToolCode→ {"result": ..., "tool_calls": 3, "tokens_saved_est": 1280, "duration_ms": 712}
```

The stubs are a deterministic projection of the session's catalog snapshot: the same
snapshot always renders byte-identical files. Each server becomes a Starlark module; each
tool a function on it (`github.list_issues(...)`).

## Writing Starlark

Snippets are Starlark — a deliberately small Python subset. They assign the final value to
`result`:

```python
issues = github.list_issues(repo="owner/repo", state="open")
recent = [i for i in issues["structuredContent"]["items"] if i["age_days"] < 7]
result = {"count": len(recent), "titles": [i["title"] for i in recent]}
```

A tool call returns the raw MCP `CallToolResult` (its `content` / `structuredContent`).
Only the deterministic, pure surface is available: the allowlisted built-ins, `json`
(encode/decode), `math`, and a frozen `time.now()`. There is **no** `import`, no `load`, no
file/network/`os`/subprocess access — see the [sandbox guarantees](#sandbox-guarantees).

## Sandbox guarantees

The runtime treats snippets as hostile input (they are written by a model that may have been
prompt-injected). The full attack/defense analysis is in
[`docs/security/code-mode-threat-model.md`](../security/code-mode-threat-model.md); in brief:

- **No escape.** `thread.Load` is nil; `load` is rejected at parse time; `import` is a parse
  error; built-ins are an **allowlist** asserted statically (any non-allowlisted identifier
  — including unwanted Universe built-ins like `getattr`/`set` — is rejected before
  execution). The only host values in scope are the pure built-ins, the three stdlib
  modules, and the tool bindings.
- **No governance bypass.** A snippet's only path to a tool is the single dispatcher seam,
  which runs the exact same `tools/call` core a direct call runs. There is no second path.
- **Budgets.** Every execution is bounded by instruction count (default 100 000), wall clock
  (30 s), output bytes (1 MiB), and tool calls (20). Defaults are conservative; operators
  raise them per route.
- **Redaction.** `print()` output and the final `result` pass through the same audit
  redactor before leaving the sandbox.
- **Determinism.** `time.now()` is frozen per execution and coarsened to the second, so
  replay (after an approval pause) is deterministic.

## Governance and approvals

Every tool call from inside a snippet emits the regular `tool_call.start` / `.complete`
audit events and a `mcp.tool_call` span, parented under the execution's `code_mode.execution`
span. Policy, scope, credential injection, and approval all apply unchanged.

When a called tool requires approval, the execution suspends, the elicitation reaches the
host as usual, and execution resumes after approval — the model never sees the Starlark
state. (The suspend/resume continuation flow ships in a follow-up unit.)

## Audit and telemetry

- Audit events: `code_mode.execution_started`, `code_mode.execution_completed`,
  `code_mode.execution_failed` (counts only — never code, tool arguments, or results), plus
  the regular `tool_call.*` events for each in-sandbox call.
- Spans: a parent `code_mode.execution` span per run; child `mcp.tool_call` spans per call.
- Each execution records an estimated tokens-saved figure — see
  [code-mode-savings](./code-mode-savings.md).

## Errors

`executeToolCode` returns typed JSON-RPC errors. The precise reason is in `error.data.code`:

| `error.code` | meaning                          | `data.code` examples                                   |
|-------------:|----------------------------------|--------------------------------------------------------|
| `-32010`     | static safety gate rejection     | `code_mode.unsafe_call` (detail names the identifier)  |
| `-32011`     | a budget tripped                 | `code_mode.budget_exceeded` (detail: which budget)     |
| `-32012`     | compile / runtime / tool error   | `code_mode.compile_error`, `code_mode.tool_error`, …   |
| `-32001`     | a called tool needs approval     | `code_mode.approval_required`                          |

## See also

- [How to use Code Mode](../how-to/use-code-mode.md)
- [Token-savings estimator](./code-mode-savings.md)
- [Threat model](../security/code-mode-threat-model.md)
- Plan: `docs/plans/phase-13.5-mcp-code-mode.md`
