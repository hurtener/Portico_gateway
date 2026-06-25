# Use Code Mode

Code Mode is an alternative tool-presentation mode in which an MCP client orchestrates work
by writing small [Starlark](https://github.com/google/starlark-go) snippets rather than
issuing individual `tools/call` requests. The client sees four meta-tools instead of the full
namespaced catalog; intermediate values stay inside a sandboxed executor; only the final
`result` value crosses back into the conversation.

For the architecture, sandbox guarantees, and token-savings model see
[Code Mode](/concepts/code-mode) and [Code Mode token savings](/concepts/code-mode-savings).
This page walks through a complete session end to end.

::: info Opt-in, per-session
Code Mode is disabled unless a client explicitly enables it during `initialize`. Existing
clients that send no opt-in capability are unaffected and see the regular namespaced tool
catalog unchanged.
:::

---

## 1. Opt in at initialize

Add the `code_mode` object inside `capabilities.experimental.portico` in your `initialize`
request:

```jsonc
// POST /mcp  (HTTP+SSE northbound transport)
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-06-18",
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
    },
    "clientInfo": { "name": "my-agent", "version": "1.0" }
  }
}
```

The three opt-in fields:

| Field            | Type    | Default    | Meaning                                                                                          |
|------------------|---------|------------|--------------------------------------------------------------------------------------------------|
| `enabled`        | boolean | —          | Required. `true` turns Code Mode on for this session.                                            |
| `binding_level`  | string  | `"server"` | `"server"` — one stub file per downstream server. `"tool"` — also one stub file per tool.        |
| `max_tool_calls` | integer | `20`       | Per-execution tool-call budget. The operator policy may apply a lower ceiling regardless.         |

Save the `Mcp-Session-Id` from the response header. Every subsequent request in this session
must carry it.

::: tip Minimal opt-in
`{"enabled": true}` is sufficient. Omitted fields use the defaults shown above.
:::

---

## 2. Inspect the meta-tools

After a Code Mode `initialize`, `tools/list` returns four meta-tools instead of the full
catalog:

```jsonc
// → tools/list
{
  "jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}
}

// ← result
{
  "tools": [
    { "name": "mcp.listToolFiles",   "description": "Enumerate the virtual .pyi stub file system for this session's tool catalog." },
    { "name": "mcp.readToolFile",    "description": "Read one virtual stub file (compact function signatures for a server or tool)." },
    { "name": "mcp.getToolDocs",     "description": "Fetch full docs (description, JSON Schema, risk class, approval policy) for one or more named tools." },
    { "name": "mcp.executeToolCode", "description": "Run a Starlark snippet that calls tools through their server modules and returns a final `result`." }
  ]
}
```

These names are reserved. No downstream server can register a tool under the `mcp.` namespace.

---

## 3. Discover tools

### List available files

`mcp.listToolFiles` enumerates the virtual file system for this session's snapshot. The file
list is deterministic for a given snapshot: the same snapshot always renders the same files.

```jsonc
// → tools/call
{
  "jsonrpc": "2.0", "id": 3, "method": "tools/call",
  "params": {
    "name": "mcp.listToolFiles",
    "arguments": {}
  }
}

// ← result
{
  "structuredContent": {
    "files": [
      "index.md",
      "servers/github.pyi",
      "servers/jira.pyi",
      "servers/slack.pyi"
    ]
  }
}
```

Start with `index.md`, which provides a short orientation listing every server and its tool
count. Then fetch only the server stubs you intend to use — this is the primary token-saving
mechanism: a 150-tool catalog never lands in the model context; only the stubs you explicitly
read do.

### Read a stub file

`mcp.readToolFile` returns the content of one virtual `.pyi` file. Each server's stub is a
compact Python type-annotated signature for every tool it exposes:

```jsonc
// → tools/call
{
  "jsonrpc": "2.0", "id": 4, "method": "tools/call",
  "params": {
    "name": "mcp.readToolFile",
    "arguments": { "path": "servers/github.pyi" }
  }
}

// ← result
{
  "structuredContent": {
    "path": "servers/github.pyi",
    "content": "# Server \"github\" exposed as Starlark module \"github\"\n# 3 tool(s). Call as: github.<function>(...)\n\ndef list_issues(repo: str, state: str = None) -> dict:\n    \"\"\"List issues in a repository.\"\"\"\n    ...\n\ndef create_issue(repo: str, title: str, body: str = None) -> dict:\n    \"\"\"Create a new issue.\"\"\"\n    ...\n\ndef close_issue(repo: str, issue_number: int) -> dict:\n    \"\"\"Close an issue by number.\"\"\"\n    ..."
  }
}
```

The stub format:
- Each downstream server becomes a Starlark **module** named after the sanitized server ID
  (e.g. `github`).
- Each tool becomes a **function** on that module (e.g. `github.list_issues`).
- Required parameters come first, then optional parameters with `= None`.
- The return type is always `-> dict` — tool calls return the raw MCP `CallToolResult`.

### Fetch full tool docs

When you need the full JSON Schema, risk class, or approval policy for a specific tool, use
`mcp.getToolDocs`:

```jsonc
// → tools/call
{
  "jsonrpc": "2.0", "id": 5, "method": "tools/call",
  "params": {
    "name": "mcp.getToolDocs",
    "arguments": { "tools": ["github.list_issues", "github.create_issue"] }
  }
}

// ← result
{
  "structuredContent": {
    "docs": [
      {
        "name": "github.list_issues",
        "found": true,
        "server_id": "github",
        "description": "List issues in a repository.",
        "input_schema": { "type": "object", "properties": { ... } },
        "risk_class": "read",
        "requires_approval": false
      },
      {
        "name": "github.create_issue",
        "found": true,
        "server_id": "github",
        "description": "Create a new issue.",
        "input_schema": { "type": "object", "properties": { ... } },
        "risk_class": "write",
        "requires_approval": true
      }
    ]
  }
}
```

The `requires_approval` field is particularly important: if a tool requires approval and you
call it from inside a snippet, the execution will suspend (see [section 5](#5-handle-approval-suspension) below).

---

## 4. Execute a snippet

`mcp.executeToolCode` runs a Starlark snippet under the sandbox and returns the value you
assigned to the `result` global:

```jsonc
// → tools/call
{
  "jsonrpc": "2.0", "id": 6, "method": "tools/call",
  "params": {
    "name": "mcp.executeToolCode",
    "arguments": {
      "code": "issues = github.list_issues(repo='owner/repo', state='open')\nrecent = [i for i in issues['structuredContent']['items'] if i['age_days'] < 7]\nresult = {'count': len(recent), 'titles': [i['title'] for i in recent]}"
    }
  }
}

// ← result
{
  "structuredContent": {
    "result": { "count": 3, "titles": ["Fix login bug", "Update deps", "Add tests"] },
    "output": "",
    "output_truncated": false,
    "tool_calls": 1,
    "tokens_saved_est": 1840,
    "duration_ms": 312
  }
}
```

### Response fields

| Field              | Type    | Meaning                                                                               |
|--------------------|---------|---------------------------------------------------------------------------------------|
| `result`           | any     | The final value assigned to `result` in the snippet. JSON `null` if never assigned.   |
| `output`           | string  | Captured `print()` output, redacted and possibly truncated.                            |
| `output_truncated` | boolean | `true` if `print()` output exceeded the 1 MiB cap.                                    |
| `tool_calls`       | integer | Number of tool calls issued from inside the snippet.                                  |
| `tokens_saved_est` | integer | Estimated tokens saved versus a plain multi-call session over the same snapshot.       |
| `duration_ms`      | integer | Wall-clock execution time in milliseconds.                                            |

### Writing Starlark snippets

Starlark is a deliberately small, deterministic Python subset. Key rules for Code Mode:

**Assign `result` before the snippet ends.** A snippet that never assigns `result` returns
JSON `null`. There is no explicit `return` statement — `result` is a global.

**Tool calls are keyword-argument only.** Positional arguments are rejected at runtime.
Match the parameter names from the stub:

```python
# Correct
issues = github.list_issues(repo="owner/repo", state="open")

# Wrong — positional arguments
issues = github.list_issues("owner/repo", "open")
```

**Tool calls return the raw MCP `CallToolResult`.** Navigate `["structuredContent"]` for
tools that return structured data, or `["content"]` for text/blob results:

```python
result_raw = github.list_issues(repo="owner/repo")
items = result_raw["structuredContent"]["items"]
result = len(items)
```

**Allowed built-ins** — the sandbox exposes an explicit allowlist, not the full Starlark
universe:

| Category        | Available names                                                                                              |
|-----------------|--------------------------------------------------------------------------------------------------------------|
| Core functions  | `len`, `range`, `enumerate`, `zip`, `sorted`, `min`, `max`, `sum`                                           |
| Type constructors | `dict`, `list`, `str`, `int`, `float`, `bool`                                                             |
| Predicates      | `any`, `all`                                                                                                  |
| Introspection   | `repr`, `type`                                                                                               |
| Output          | `print` (captured, redacted, capped at 1 MiB)                                                               |
| Constants       | `None`, `True`, `False`                                                                                      |
| Modules         | `json` (`json.encode`, `json.decode`), `math` (all pure functions), `time` (`time.now()`, frozen per run)   |

**Not available:** `import`, `load`, `open`, `os`, `set`, `getattr`, `hasattr`, `dir`,
`bytes`, `chr`, `ord`, `hash`, `reversed`, `fail`, `abs`, file I/O, network access,
subprocess. Using any forbidden identifier causes `code_mode.unsafe_call` before execution
begins.

**Loops are allowed.** `while` and top-level control flow are enabled. The step budget (100k
instructions by default) and wall-clock budget (30 seconds) bound non-termination.

**`time.now()` is frozen.** The clock is coarsened to the second and fixed at execution start.
This guarantees deterministic replay after an approval pause.

---

## 5. Handle approval suspension

When a tool called from inside a snippet requires operator approval, the execution
**suspends** rather than failing. The meta-tool returns a structured response with
`status: "approval_required"`:

```jsonc
// ← result (tool inside snippet required approval)
{
  "structuredContent": {
    "status": "approval_required",
    "approval_id": "appr_01j9x...",
    "continuation_token": "cmct_01j9y...",
    "tool": "github.create_issue",
    "tool_calls": 2
  }
}
```

The execution has not failed — it has suspended cleanly. All tool calls that completed before
the approval-required call are cached server-side. The snippet itself is also persisted.

### Approval round trip

1. **Surface the approval to the operator.** The `approval_id` from the response is the
   approval Portico opened via the standard [approval flow](/concepts/approvals). Present it
   to the operator through whatever UI or `elicitation/create` mechanism your host uses.

2. **Operator approves.** The approval record is updated on the gateway side.

3. **Resume the execution.** Call `mcp.executeToolCode` again with `continuation_token`
   instead of `code`:

```jsonc
// → tools/call  (resume after approval)
{
  "jsonrpc": "2.0", "id": 7, "method": "tools/call",
  "params": {
    "name": "mcp.executeToolCode",
    "arguments": {
      "continuation_token": "cmct_01j9y..."
    }
  }
}
```

The gateway replays the snippet from the beginning, serving cached results for the calls that
already ran (so prior write-side-effect tools do not execute twice), then re-dispatches the
approval-required call with the granted approval wired in. If another tool in the same snippet
also requires approval, the execution suspends again and issues a new `continuation_token`.

::: warning Continuation tokens are single-use and tenant-scoped
A continuation token is bound to the tenant and session that created it. Presenting a token
from a different tenant fails with `code_mode.continuation_not_found`. Replaying the same
token twice fails with `code_mode.double_resume`. If the snapshot has changed between
suspension and resume, the resume fails with `code_mode.snapshot_drifted`.
:::

---

## 6. Handle errors

`mcp.executeToolCode` returns a typed JSON-RPC error when execution cannot proceed. The
top-level `error.code` groups the class; `error.data.code` names the exact reason.

| `error.code` | `error.data.code`                  | What happened                                               | What to do                                                     |
|--------------|------------------------------------|-------------------------------------------------------------|----------------------------------------------------------------|
| `-32010`     | `code_mode.unsafe_call`            | A forbidden identifier was used; `data.detail` names it.    | Remove the identifier; consult the allowlist above.            |
| `-32010`     | `code_mode.disabled_by_policy`     | Code Mode is disabled on this tenant by operator policy.    | Contact your operator.                                         |
| `-32010`     | `code_mode.binding_level_denied`   | The session's `binding_level` is not permitted by policy.   | Use `"server"` binding level or ask the operator to allow it.  |
| `-32010`     | `code_mode.execution_too_large`    | The snippet exceeds the operator-configured size limit.     | Split the snippet into smaller calls.                          |
| `-32011`     | `code_mode.budget_exceeded`        | A budget tripped; `data.detail` names which one.            | See the budget table below.                                    |
| `-32012`     | `code_mode.compile_error`          | The snippet failed to parse (syntax error).                 | Fix the Starlark syntax.                                       |
| `-32012`     | `code_mode.runtime_error`          | A Starlark runtime error (type error, `fail()`, etc.).      | Inspect the error message; fix the snippet logic.              |
| `-32012`     | `code_mode.tool_error`             | An in-sandbox tool call failed (policy deny or downstream). | Check the tool name, arguments, and your JWT scopes.           |
| `-32001`     | `code_mode.approval_required`      | A tool requires approval; `continuation_token` is issued.   | Follow the approval round trip in section 5.                   |

### Budget dimensions

When `error.data.code` is `code_mode.budget_exceeded`, `error.data.detail` names the tripped
dimension:

| `detail`        | Default limit     | How to adjust                                                          |
|-----------------|-------------------|------------------------------------------------------------------------|
| `steps`         | 100,000 steps     | Break up computation; avoid tight loops over large collections.        |
| `wall_clock`    | 30 seconds        | Reduce the number or latency of tool calls per snippet.                |
| `tool_calls`    | 20 per execution  | Increase `max_tool_calls` at opt-in, subject to the policy ceiling.    |
| `output_bytes`  | 1 MiB             | Reduce `print()` volume; use `result` rather than printing everything. |
| `memory`        | 256 MiB           | Avoid unbounded list/string growth; use counts or summaries.           |

A zero value for any budget field normalizes to the conservative default — a zero budget
can never mean "unlimited."

---

## 7. Adjust the tool-call budget at opt-in

To raise the default 20-call budget for a session, pass `max_tool_calls` in the opt-in:

```jsonc
{
  "capabilities": {
    "experimental": {
      "portico": {
        "code_mode": {
          "enabled": true,
          "max_tool_calls": 50
        }
      }
    }
  }
}
```

The operator policy applies a ceiling. If the policy cap is lower than your request, the
lower value is used silently — a snippet that trips the tool-call budget will return
`code_mode.budget_exceeded` with `detail: "tool_calls"`.

---

## 8. Operator CLI

Operators with `admin` scope can inspect Code Mode state or run a snippet against a live
session from the command line:

```bash
# Render the virtual stub file system for a session (does not consume a tool call)
portico code-mode render --tenant <tenant_id> --session <session_id>

# Execute a snippet against a live session
portico code-mode exec \
  --tenant <tenant_id> \
  --session <session_id> \
  --code @snippet.star
```

These commands share the same governed execution path as a client-issued `mcp.executeToolCode`
call — they are not a bypass.

::: info CLI shipping status
The `code-mode` subcommands ship in a follow-up unit. Run `portico code-mode --help` to
confirm availability on your build.
:::

---

## 9. Full worked example

The following exchange shows the entire flow — opt-in, discover, execute — against a
hypothetical `github` and `slack` server:

```jsonc
// 1. Initialize with Code Mode
// → {"method":"initialize","params":{"capabilities":{"experimental":{"portico":{"code_mode":{"enabled":true}}}},...}}
// ← {"result":{"capabilities":{"experimental":{"portico":{"code_mode":{"enabled":true}}}},...}}

// 2. List available stubs
// → {"method":"tools/call","params":{"name":"mcp.listToolFiles","arguments":{}}}
// ← {"result":{"structuredContent":{"files":["index.md","servers/github.pyi","servers/slack.pyi"]}}}

// 3. Read the github stub
// → {"method":"tools/call","params":{"name":"mcp.readToolFile","arguments":{"path":"servers/github.pyi"}}}
// ← {"result":{"structuredContent":{"path":"servers/github.pyi","content":"def list_issues(repo: str, ...) -> dict: ..."}}}

// 4. Execute a multi-tool snippet
// → {"method":"tools/call","params":{"name":"mcp.executeToolCode","arguments":{"code":
//     "issues = github.list_issues(repo='owner/repo', state='open')\n
//      count = len(issues['structuredContent']['items'])\n
//      slack.post_message(channel='#ops', text='Open issues: ' + str(count))\n
//      result = {'issues_found': count}"}}}
// ← {"result":{"structuredContent":{
//     "result": {"issues_found": 7},
//     "output": "",
//     "output_truncated": false,
//     "tool_calls": 2,
//     "tokens_saved_est": 2240,
//     "duration_ms": 480
//   }}}
```

Two downstream tool calls collapse into one `executeToolCode` round trip. The full catalog
schema never lands in the model context — only the two stub files read in steps 2–3.

---

## Related

- [Code Mode](/concepts/code-mode) — architecture, sandbox guarantees, and governance model
- [Code Mode token savings](/concepts/code-mode-savings) — how the `tokens_saved_est` figure is calculated
- [Approvals](/concepts/approvals) — the approval flow that Code Mode suspension integrates with
- [MCP Northbound](/concepts/mcp-northbound) — the HTTP+SSE transport Code Mode sessions run over
- [Catalog and Sessions](/concepts/catalog-and-sessions) — how session snapshots work and what the projector reads
- [Agent Profiles](/concepts/agent-profiles) — per-consumer tool and scope entitlement that governs in-sandbox calls
- [Observability](/concepts/observability) — `code_mode.execution` spans and audit events emitted per run
