# How to use Code Mode

This guide walks an MCP client through a Code Mode session end to end. For the concept and
the governance guarantees, see [Code Mode](../concepts/code-mode.md).

## 1. Opt in at initialize

Send the Code Mode capability in your `initialize` request:

```jsonc
// → POST /mcp
{
  "jsonrpc": "2.0", "id": 1, "method": "initialize",
  "params": {
    "protocolVersion": "2025-06-18",
    "capabilities": {
      "experimental": { "portico": { "code_mode": { "enabled": true } } }
    },
    "clientInfo": { "name": "my-agent", "version": "1.0" }
  }
}
```

Keep the `Mcp-Session-Id` response header — every subsequent request uses it.

## 2. See the meta-tools

`tools/list` now returns the four `mcp.*` meta-tools instead of the namespaced catalog:

```jsonc
// → tools/list
// ← { "tools": [ {"name": "mcp.listToolFiles"}, {"name": "mcp.readToolFile"},
//                {"name": "mcp.getToolDocs"}, {"name": "mcp.executeToolCode"} ] }
```

## 3. Discover the tools you need

```jsonc
// → tools/call mcp.listToolFiles {}
// ← { "structuredContent": { "files": ["index.md", "servers/github.pyi", ...] } }

// → tools/call mcp.readToolFile { "path": "servers/github.pyi" }
// ← { "structuredContent": { "content": "def list_issues(repo: str, ...) -> dict: ..." } }
```

Read `index.md` first for orientation, then the server stub(s) you'll use. Only the stubs
you fetch land in your context — that's the token win. Use `mcp.getToolDocs` when you need a
tool's full JSON schema, risk class, or approval policy.

## 4. Execute a snippet

Write Starlark that calls tools and assigns `result`:

```jsonc
// → tools/call mcp.executeToolCode
{
  "name": "mcp.executeToolCode",
  "arguments": {
    "code": "issues = github.list_issues(repo='owner/repo')\nresult = len(issues['structuredContent']['items'])"
  }
}
// ← {
//   "structuredContent": {
//     "result": 12, "tool_calls": 1, "tokens_saved_est": 1840, "duration_ms": 240,
//     "output": "", "output_truncated": false
//   }
// }
```

### Tips for writing snippets

- Assign your final answer to `result`. A missing `result` returns JSON `null`.
- Tool calls take **keyword arguments** matching the stub signature
  (`github.list_issues(repo="...")`), never positional.
- A tool call returns the raw MCP result — navigate `["structuredContent"]` or
  `["content"]`.
- Available helpers: the allowlisted built-ins (`len`, `range`, `sorted`, `sum`, `min`,
  `max`, `any`, `all`, `enumerate`, `zip`, `dict`, `list`, `str`, `int`, `float`, `bool`,
  `repr`, `type`, `print`), `json.encode` / `json.decode`, `math`, and `time.now()`.
- **Not** available: `import`, `load`, `open`, `os`, `set`, `getattr`, file/network access.
  Using them returns `code_mode.unsafe_call`.

## 5. Handle errors

`executeToolCode` returns a JSON-RPC error with the specific reason in `error.data.code`:

| `error.data.code`            | what happened                                        | what to do                              |
|------------------------------|------------------------------------------------------|-----------------------------------------|
| `code_mode.unsafe_call`      | you used a forbidden identifier (named in `detail`)  | remove it; stick to the allowlist       |
| `code_mode.compile_error`    | the snippet didn't parse                             | fix the Starlark syntax                 |
| `code_mode.budget_exceeded`  | a budget tripped (`detail`: steps/wall_clock/…)      | do less work per execution; split it up |
| `code_mode.tool_error`       | a tool call failed (policy deny, downstream error)   | inspect the tool; adjust args/scope     |
| `code_mode.approval_required`| a called tool needs operator approval                | surface the approval, then retry        |

## 6. Budgets

Defaults per execution: 100 000 instructions, 30 s wall clock, 1 MiB of `print()` output, 20
tool calls. To raise the tool-call budget for a session, set `max_tool_calls` in the opt-in.
Operators can tighten or loosen budgets per route via policy.

## CLI

For debugging, an operator with `admin` scope can render the stubs or run a snippet against a
live session:

```bash
portico code-mode render --tenant <id> --session <id>
portico code-mode exec   --tenant <id> --session <id> --code @snippet.star
```

(The CLI ships in a follow-up unit.)
