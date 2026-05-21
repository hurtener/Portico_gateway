# Phase 13.5 — MCP Code Mode

> Self-contained implementation plan. Builds on Phases 0–13. **Post-V1.5.** Ports the pattern Bifrost popularised as *MCP Code Mode* into Portico's own MCP DNA: a virtualised, code-driven tool surface that replaces token-heavy catalogs with four discovery/execution meta-tools and a sandboxed runtime, cutting LLM token spend on tool calls by roughly 50% and round-trip count by 30–40% on multi-tool sessions.

## Goal

Add **Code Mode** as a *per-session, per-tenant, opt-in* alternative tool-presentation mode for MCP clients. When enabled, a session sees four meta-tools instead of the literal catalog:

| Meta-tool         | Purpose                                                              |
|-------------------|----------------------------------------------------------------------|
| `mcp.listToolFiles`   | enumerate the virtual `.pyi` "stub" file system for the snapshot   |
| `mcp.readToolFile`    | read a stub file (compact function signatures for a server or tool)|
| `mcp.getToolDocs`     | fetch full docs / JSON Schema for one or more named tools          |
| `mcp.executeToolCode` | run a Starlark snippet that calls tools and returns a final result |

The model orchestrates work by writing Starlark code that calls tools through bindings exposed in the sandbox. Intermediate results stay inside the sandbox; only the final `result` value crosses back into the conversation. This means a 200-tool catalog never lands in the model's context window; only the stubs the model *chose to load* land, and only the meta-tools' signatures are present by default.

Phase 13.5 binds the pattern to Portico's existing primitives — snapshots, namespacing, policy, approval flow, audit, redaction — so Code Mode is not a parallel universe; it is a different *projection* of the same catalog with the same governance.

## Why this phase exists

The Bifrost docs measured the savings against typical multi-server agents: a session that talks to 8–10 MCP servers (≈150 tools) burns most of the model's context budget on tool definitions and round-trip framing. Code Mode cuts that to four meta-tool definitions plus the on-demand stubs the model fetches, and replaces N round trips (one per tool call) with one larger round trip that executes Starlark calling tools in a loop.

This is a substantial UX and unit-economics win for Portico's customers. We do not, however, need to depend on Bifrost's implementation to get it. The pattern itself is the value; the implementation is a Starlark interpreter + a virtual catalog. Both are CGo-free Go libraries we can adopt without coupling to Bifrost's product shape:

- **Starlark in Go** — `go.starlark.net` is the canonical pure-Go Starlark interpreter (used by Bazel, Buck2). MIT/Apache. No CGo.
- **Virtual `.pyi` catalog** — derived from our existing snapshot model (Phase 6). We already namespace tools per session and produce structured tool definitions; rendering them as Python stub files is a presentation layer.

Doing this work in our own DNA — not as a Bifrost passthrough — means:

1. Tenant isolation and policy run on every tool call the sandbox issues, not just at session boundaries.
2. The approval flow (Phase 5) still applies — the sandbox short-circuits with a structured wait when a called tool needs approval, the elicitation reaches the calling host, and execution resumes after approval.
3. Audit captures every tool dispatched from inside a code-mode execution, with the originating `code_mode.execution_id` as a parent span.
4. The Skill Pack runtime (Phase 4) can ship Code-Mode-aware Skills: a manifest declares "this Skill is intended for Code Mode" and ships canonical Starlark snippets the model can adapt.

## Prerequisites

- Phase 1 MCP dispatcher + snapshot model.
- Phase 4 Skill Pack runtime (Skills can opt into Code Mode metadata).
- Phase 5 policy + approval + vault — every tool call from inside the sandbox goes through the same checks.
- Phase 6 catalog snapshots — Code Mode operates *over* a snapshot, never over live state.
- Phase 11 telemetry — sandbox executions land as a parent span with tool-call children.
- Phase 13 LLM gateway — the playground gains a Code Mode toggle. Code Mode itself is independent of the LLM gateway (it speaks MCP, not OpenAI), but the playground is where most operators will discover it.

## Out of scope (explicit)

- **No Python interpreter.** Starlark is a Python *subset* — deliberately so. No `import`, no classes, no file I/O, no network from inside the sandbox. Operators who want full Python should run a Code Interpreter MCP server *behind* Portico; Phase 13.5 is the lightweight, governance-bound runtime, not a remote-execution platform.
- **No Bifrost dependency.** The pattern is reused; the code is not. We adopt the four meta-tool names and the `.pyi` stub vocabulary because they are becoming the de-facto standard, but the implementation is Portico-native.
- **No automatic Code Mode opt-in.** Sessions get Code Mode only when the client requests it at `initialize` time via a `_meta.portico.code_mode` capability, or when an operator policy elevates them. Existing clients see the catalog they always saw.
- **No multi-language sandbox.** Starlark only. JavaScript / Lua / WebAssembly hosts are post-V2.
- **No persistent sandbox state across calls.** Each `executeToolCode` invocation is fresh. State is what the model writes into its messages.
- **No GPU / external-resource access from the sandbox.** Starlark calls only what we expose: the namespaced tool functions, `print()`, and a handful of safe stdlib helpers (`json`, `math`, `time` — wall-clock read only, no sleep).

## Deliverables

1. **`internal/mcp/codemode/`** — the runtime: virtual file system, meta-tool handlers, Starlark host, snapshot projector, audit/span emitter.
2. **`internal/mcp/codemode/runtime/`** — Starlark execution engine. Wraps `go.starlark.net` with our sandbox policy (no `load`, no `import`, no file I/O, restricted built-ins, per-execution memory + time + step budget). Exposes tool functions as Starlark callables.
3. **`internal/mcp/codemode/catalog/`** — virtual-FS projector. Given a snapshot, renders:
   - `servers/<server>.pyi` — one stub per server (default binding level).
   - `servers/<server>/<tool>.pyi` — optional per-tool stubs (configurable binding level).
   - `index.md` — short orientation file.
   The projector is deterministic: same snapshot → byte-identical stubs.
4. **Meta-tool handlers** — `mcp.listToolFiles`, `mcp.readToolFile`, `mcp.getToolDocs`, `mcp.executeToolCode`. They live in `internal/mcp/codemode/handlers.go` and register with the existing MCP dispatcher under a reserved namespace (`mcp.*` — see §13 forbidden practices addition).
5. **Per-session opt-in** — `initialize` accepts a `_meta.portico.code_mode: { enabled: true, binding_level: "server"|"tool", auto_execute: bool }` capability. The session's tool projection becomes the four meta-tools instead of the namespaced catalog.
6. **Policy extension** — new matchers (`code_mode.enabled`, `code_mode.binding_level`, `code_mode.execution_size_bytes`, `code_mode.tool_calls_inside`) and new actions (`require_approval_on_executeToolCode`, `deny_on_unsafe_starlark`). The default policy is open within tenant; operators tighten.
7. **Approval flow integration** — when a tool called from inside the sandbox requires approval, the runtime suspends the sandbox (snapshots its Starlark frame to a serialised continuation), emits the elicitation as it normally would, and resumes the sandbox when the operator approves. If the client doesn't support elicitation, the runtime returns a structured `approval_required` error with the suspended-execution token so the model can retry after the operator acts. (The continuation lives in our DB; the model never sees Starlark state.)
8. **Audit + telemetry** — `code_mode.execution_started`, `code_mode.execution_completed`, `code_mode.execution_failed`, `code_mode.tool_called` (one per sandbox-triggered call), `code_mode.budget_exceeded`. Spans: parent span per execution; child spans per tool call.
9. **Skill Pack metadata** — manifests gain an optional `code_mode:` block listing canonical Starlark snippets the Skill exposes (e.g. "find recent issues across these tools" snippet). The skills runtime surfaces them in `/skills/<id>` Console detail; the playground offers a one-click insert.
10. **Console screens** — a new tab on `/playground` (Phase 10) called *Code Mode*. Renders the virtual FS as a tree, exposes a Starlark editor (Monaco), runs `executeToolCode` against the current session, and streams print() output + tool-call spans into the right rail. A new dashboard surface `/observability/code-mode` aggregates: total executions per day, tokens-saved-vs-catalog estimate, top called tools, top error patterns.
11. **CLI** — `portico code-mode render --tenant <id> --session <id>` dumps the projected stubs for a snapshot (useful for debugging policy or sharing with an external consumer). `portico code-mode exec --tenant <id> --session <id> --code @file.star` runs a snippet against a live session for testing — gated behind `admin` scope.
12. **Smoke** — `scripts/smoke/phase-13.5.sh` covers a session lifecycle: opt into Code Mode, `listToolFiles`, `readToolFile`, `executeToolCode` with a snippet that calls two tools in sequence, capture the result, assert tool-call audit events and span children. Negative: a snippet that violates the sandbox policy (`import os`) returns a typed error.
13. **Token-savings tracker** — every execution records the estimated tokens-saved vs. a hypothetical full-catalog turn (a deterministic calculation; documented and unit-tested). Rolled up into the `/observability/code-mode` dashboard so operators can put a number on the Code Mode ROI per tenant.

## Acceptance criteria

1. **Opt-in is per-session.** A session that doesn't request `code_mode` sees the regular namespaced catalog (no behavioural drift for existing clients).
2. **`listToolFiles` enumerates the virtual FS.** Returns the deterministic projection of the active snapshot (server-level by default, tool-level when requested).
3. **`readToolFile` returns valid Python-stub syntax.** Output is `.pyi` shape: one function-signature line per tool, with type annotations from the JSON Schema. The schema → stub translator is unit-tested for every supported JSON Schema type.
4. **`getToolDocs` returns full docs.** Includes the tool description, JSON Schema, side-effect class, required scopes, approval policy. JSON.
5. **`executeToolCode` happy path.** A snippet `result = github.list_issues(repo="owner/r")` returns the tool's response as the `result` field of the meta-tool's response. The intermediate Starlark trace is captured in the span.
6. **Sandbox limits enforced.** Default budget: 30 s wall clock, 100 MB heap (Starlark guards), 100,000 instructions. Exceeding any returns `code_mode.budget_exceeded` with the budget that tripped.
7. **Sandbox safety enforced.** `load`, `import`, file I/O, network primitives are absent. Tests assert that a snippet attempting any of these returns `code_mode.unsafe_call` with the call name.
8. **Tool calls inside the sandbox go through the full envelope.** Tenant, JWT scope, policy, approval, vault credential injection, audit redaction, telemetry — every tool call from `executeToolCode` produces the same audit + span shape as a direct `tools/call` would have.
9. **Approval flow respected.** A snippet that calls a `requires_approval` tool causes the runtime to suspend, emit an elicitation (or a structured `approval_required` error), and resume after operator approval. The resumed execution returns to the same call site with the approved result.
10. **Per-tenant isolation.** Tenant A's snapshot stubs are not visible to tenant B. A `listToolFiles` call carrying tenant A's session ID returns A's projection only. Cross-tenant integration test passes.
11. **Token-savings tracker accurate.** For a synthetic conversation that would have shipped 50 tool definitions in plain mode, the tracker reports tokens-saved within 10% of the analytical answer. Documented formula in `docs/concepts/code-mode-savings.md`.
12. **Console parity.** The Playground Code Mode tab boots the snippet against a live session, streams output, shows the span tree. A Playwright spec covers the happy path and a sandbox-rejection case.
13. **Smoke gate.** `scripts/smoke/phase-13.5.sh` shows OK ≥ 12, FAIL = 0; prior phases' smokes still pass.
14. **Coverage.** `internal/mcp/codemode/...` ≥ 85% overall, ≥ 90% in `runtime/` (this is the security-critical sub-package).

## Architecture

### Package layout

```
internal/mcp/codemode/
├── codemode.go              # facade: NewSession(snapshot, opts) -> Session
├── handlers.go              # MCP method handlers for the four meta-tools
├── opts.go                  # SessionOpts (binding_level, budgets, auto_execute)
├── policy.go                # policy-extension matchers/actions
├── catalog/
│   ├── projector.go         # snapshot -> virtual FS
│   ├── stubs.go             # JSON Schema -> Python type annotation
│   └── projector_test.go
├── runtime/
│   ├── sandbox.go           # Starlark host setup: built-ins allowlist, globals, recursion limit
│   ├── bindings.go          # exposes tool functions to Starlark
│   ├── budget.go            # wall-clock + instruction-count + memory budget enforcement
│   ├── continuation.go      # serialise/resume Starlark frame on approval pause
│   └── sandbox_test.go
├── audit.go                 # span + audit emit helpers (consistent attributes)
├── savings.go               # tokens-saved-vs-catalog estimator
└── api_console.go           # /api/code-mode/* read endpoints for Console

internal/storage/sqlite/migrations/
└── 0016_code_mode.sql       # code_mode_executions, code_mode_continuations tables

cmd/portico/
└── cmd_code_mode.go         # `portico code-mode render|exec`

web/console/src/routes/playground/code-mode/
├── +page.svelte             # editor + FS tree + run + output panes
└── playground.spec.ts       # Playwright

web/console/src/routes/observability/code-mode/
├── +page.svelte             # tokens-saved dashboard, top tools, errors
└── observability.spec.ts
```

### Request lifecycle (a Code-Mode session)

```
client → initialize  (_meta.portico.code_mode = {enabled: true, binding_level: "server"})
        ←   capabilities advertise four meta-tools instead of namespaced catalog

client → tools/call mcp.listToolFiles
        ←   ["servers/github.pyi", "servers/jira.pyi", "servers/slack.pyi", ...]

client → tools/call mcp.readToolFile {path: "servers/github.pyi"}
        ←   "def list_issues(repo: str, state: str = 'open') -> dict: ...
             def comment_on(repo: str, issue: int, body: str) -> dict: ..."

client → tools/call mcp.executeToolCode {code: "result = github.list_issues(repo='x')\n..."}
            ┌──────────────────────────────────────────────────────────────┐
            │ runtime opens span "code_mode.execution"                     │
            │ binds tool functions onto Starlark globals (per snapshot)    │
            │ executes; each tool call:                                    │
            │   1. resolve namespaced tool from current snapshot           │
            │   2. evaluate policy (tenant, scope, approval, redaction)    │
            │   3. dispatch via existing internal/mcp/dispatcher           │
            │   4. record child span + audit event                         │
            │ if approval required: serialise continuation, return         │
            │   structured wait; the resume path re-binds the same         │
            │   snapshot and continues from the saved frame                │
            │ on completion: return `result` + token-savings estimate      │
            └──────────────────────────────────────────────────────────────┘
        ←   {result: ..., tokens_saved_est: 1280, tool_calls: 3, duration_ms: 712}
```

The runtime never invents tool calls; every dispatch goes through `internal/mcp/dispatcher`, which already enforces tenancy, namespacing, snapshots, policy, approval, vault credential injection, audit, and telemetry.

### Sandbox guarantees

Built on `go.starlark.net`, with the following hardening:

- **No `load()` statement.** The Starlark thread is configured with `Load: nil`.
- **No `import`.** Starlark does not have `import` natively; the safety property is asserted at parse time by rejecting any module-level identifier we haven't explicitly bound.
- **Allowlist built-ins.** `print`, `len`, `range`, `enumerate`, `zip`, `sorted`, `min`, `max`, `sum`, `dict`, `list`, `str`, `int`, `float`, `bool`, `any`, `all`, `repr`, `type`. `json.encode`/`json.decode` from `go.starlark.net/lib/json`. `math` from `go.starlark.net/lib/math`. Nothing else.
- **No `time.sleep`, no `time.now` with sub-second precision.** Read-only `time.now` returning a Starlark `time` value; coarsened to second resolution to prevent timing side-channels.
- **Budgets.**
  - `MaxSteps` (Starlark's built-in step limit) — default 100,000, configurable per route.
  - `WallClock` — default 30 s; runtime cancels via `context.Context`.
  - `MaxOutputBytes` — `print()` buffered; if more than 1 MB accumulated, the rest is dropped with an audit event.
  - `MaxToolCalls` — default 20 per execution; configurable.
- **No host file system, no `os` package, no `subprocess`.** Reading other tool files (`mcp.readToolFile`) is a meta-tool call routed back through the MCP dispatcher, not a Starlark file read.

### Continuation on approval

When a tool inside the sandbox needs approval, we need to suspend the Starlark execution and resume from the same call site after the approval lands. Starlark's `Thread` is not natively serialisable, so we use a "structured continuation" pattern:

1. The runtime detects `approval_required` from the dispatcher.
2. It records `(execution_id, tool_called, args, current_locals_snapshot, code_position, accumulated_print_buffer)` in `code_mode_continuations`.
3. It returns `executeToolCode` with `status: "approval_required"`, an `approval_id`, and the `continuation_token`.
4. The model surfaces the approval (or the host receives the elicitation).
5. When the operator approves, the runtime is invoked with `executeToolCode(continuation_token=...)`; it re-binds the snapshot, re-executes deterministically up to the suspended call site (re-using cached results for prior tool calls in the same execution), substitutes the approved result, and continues.

Deterministic replay relies on:
- The snapshot being immutable for the execution's lifetime (already true in Phase 6).
- Prior tool-call results within the same execution being cached in the continuation row.
- Starlark being deterministic given identical bindings.

If the snapshot drifted between suspension and resume, the runtime fails closed with `code_mode.snapshot_drifted` and the operator must re-run.

### Token-savings estimator

For each execution:

```
savings = (catalog_render_tokens(snapshot) - meta_tools_render_tokens())
        + (num_tool_calls * per_call_overhead_tokens)
        - (executed_code_tokens + executed_results_tokens)
```

`catalog_render_tokens` is the tokenised length of the OpenAI tool-definitions blob the model would have seen in plain mode. We approximate with a simple BPE-shaped estimator (4 chars/token on English text, slight adjustment for JSON). `per_call_overhead_tokens` is the empirical "model thinking between tool calls" overhead measured in Phase 11's replay corpus. The formula is documented in `docs/concepts/code-mode-savings.md`; tests assert deterministic outputs on fixed snapshots.

### SQL DDL — Migration 0016

```sql
CREATE TABLE IF NOT EXISTS code_mode_executions (
    tenant_id     TEXT NOT NULL,
    execution_id  TEXT NOT NULL,
    session_id    TEXT NOT NULL,
    started_at    TEXT NOT NULL,
    finished_at   TEXT,
    status        TEXT NOT NULL,                 -- 'running'|'completed'|'failed'|'awaiting_approval'
    snippet_sha   TEXT NOT NULL,                 -- hash of executed code (for replay / abuse review)
    tool_calls    INTEGER NOT NULL DEFAULT 0,
    tokens_saved_est INTEGER NOT NULL DEFAULT 0,
    output_redacted TEXT,                        -- redacted summary, not full body
    span_id       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, execution_id)
);
CREATE INDEX IF NOT EXISTS idx_code_mode_exec_by_session ON code_mode_executions(tenant_id, session_id, started_at DESC);

CREATE TABLE IF NOT EXISTS code_mode_continuations (
    tenant_id          TEXT NOT NULL,
    continuation_token TEXT NOT NULL,            -- ULID
    execution_id       TEXT NOT NULL,
    session_id         TEXT NOT NULL,
    snapshot_id        TEXT NOT NULL,
    code               TEXT NOT NULL,            -- the snippet (already redacted on write)
    cached_results     TEXT NOT NULL DEFAULT '[]', -- JSON array of {call_index, result_json}
    awaiting_call_index INTEGER NOT NULL,
    awaiting_approval_id TEXT,
    print_buffer       TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, continuation_token),
    FOREIGN KEY (tenant_id, execution_id)
        REFERENCES code_mode_executions(tenant_id, execution_id) ON DELETE CASCADE
);
```

### MCP wire shape

Meta-tools register under the reserved `mcp.` namespace. Their tool definitions are advertised on `tools/list` only when the session has Code Mode enabled. Standard tool-definition shape; no protocol extension. Each meta-tool's `inputSchema` is a stable JSON Schema documented in `docs/concepts/code-mode.md`.

`initialize`-time opt-in shape:

```json
{
  "params": {
    "_meta": {
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

The server-side validator rejects unknown `code_mode` fields. The shape is versioned via the protocol version — bumping is an RFC change per §8 of `AGENTS.md`.

## Implementation walkthrough

### Step 1 — Catalog projector + stubs

Pure-function module: snapshot → file map. Generates `servers/<server>.pyi` by default, optionally `servers/<server>/<tool>.pyi`. Translates JSON Schema → Python type annotations using a deterministic mapping (strings → `str`, integers → `int`, numbers → `float`, booleans → `bool`, arrays → `list`, objects → `dict`, oneOf → `Union`). Unit tests cover every supported schema feature; tests assert byte-identical output for byte-identical input.

### Step 2 — Sandbox + bindings

Wrap `go.starlark.net`. Configure the thread: `Load: nil`, allowlisted built-ins (`bindings.go`). Expose tool functions as Starlark `*starlark.Builtin` callables; each call invokes the MCP dispatcher through the same code path a direct `tools/call` uses, with a `code_mode.execution_id` tagged on the context for span correlation. Budget enforcement via `thread.SetMaxExecutionSteps` + `context.WithTimeout` + `MaxToolCalls` counter.

Tests: every safety property has a positive ("the safe thing works") and negative ("the unsafe thing is rejected with a specific error") test.

### Step 3 — Meta-tool handlers

`listToolFiles` / `readToolFile` / `getToolDocs` are thin wrappers over the projector. `executeToolCode` is the runtime entry point; it:

1. Opens an execution row in `code_mode_executions`.
2. Opens the parent span.
3. Initialises the sandbox with the active snapshot's bindings.
4. Runs the code; collects `result`, print output, tool-call counts, savings estimate.
5. Handles approval suspension via the continuation flow (Step 4).
6. Closes the span + execution row.

### Step 4 — Continuation / resume

Implement the suspend/resume cycle. When the dispatcher returns `approval_required`, the runtime serialises a continuation row (execution_id, code, cached prior call results indexed by call ordinal, the call index that is awaiting approval, the print buffer). The handler returns `status: "approval_required"` to the client. Re-invocation of `executeToolCode` with `continuation_token=...` reloads the row, re-executes the code deterministically, and substitutes the cached results for prior calls + the now-approved result for the awaited call.

Tests: snapshot-drift detection during resume; double-resume detection; resume after the continuation TTL expires.

### Step 5 — Session opt-in + dispatcher integration

Extend `internal/mcp/protocol` with the `_meta.portico.code_mode` capability shape. Extend the session record to carry a `code_mode: CodeModeOpts` field. The `tools/list` handler branches on that field: Code Mode sessions see the four meta-tools; non-Code-Mode sessions see the regular namespaced catalog (unchanged behaviour).

### Step 6 — Policy extension

`internal/mcp/codemode/policy.go` adds matchers and actions. Wire them into the existing policy engine. Tests cover each new matcher.

### Step 7 — Skill Pack metadata

Extend `internal/skills/manifest` with an optional `code_mode:` block (canonical snippets). The skills runtime indexes them; the Console renders them in `/skills/<id>` detail and offers a one-click "Insert into Code Mode editor" in the Playground.

### Step 8 — Console screens

Playground gains a Code Mode tab with a Monaco editor (Starlark syntax, no LSP — Starlark grammar bundled). Left rail: virtual FS tree (built from `listToolFiles`). Right rail: result, tokens-saved, span tree, audit events. New `/observability/code-mode` dashboard: cards for total executions today, total tokens-saved-est this month, top 5 tools called from inside Code Mode, top 5 rejection reasons.

Playwright covers the happy path + sandbox rejection + approval-suspension flow against a mock approval flow.

### Step 9 — CLI

`portico code-mode render` and `portico code-mode exec`. The latter is gated behind `admin` scope and emits an audit event recording the operator and the snippet.

### Step 10 — Smoke + tests

`scripts/smoke/phase-13.5.sh` exercises every meta-tool, the sandbox-policy guards, the approval pause/resume, and the per-tenant isolation.

## Test plan

### Unit

- `internal/mcp/codemode/catalog/projector_test.go`
  - `TestProjector_ServerLevel_Deterministic`
  - `TestProjector_ToolLevel_OneFilePerTool`
  - `TestStubs_AllJSONSchemaTypesMap`
  - `TestStubs_UnionTypes_RenderUnion`
- `internal/mcp/codemode/runtime/sandbox_test.go`
  - `TestSandbox_AllowedBuiltinsWork`
  - `TestSandbox_LoadStatementRejected`
  - `TestSandbox_NoFileSystemAccess`
  - `TestSandbox_StepBudgetEnforced`
  - `TestSandbox_WallClockBudgetEnforced`
  - `TestSandbox_MaxToolCallsEnforced`
  - `TestSandbox_PrintBufferTruncation`
- `internal/mcp/codemode/runtime/continuation_test.go`
  - `TestContinuation_SuspendsOnApprovalRequired`
  - `TestContinuation_ResumesWithCachedResults`
  - `TestContinuation_RejectsSnapshotDrift`
  - `TestContinuation_DoubleResumeRejected`
  - `TestContinuation_TTLExpiry`
- `internal/mcp/codemode/savings_test.go`
  - `TestSavings_DeterministicForFixedSnapshot`
  - `TestSavings_ApproachesAnalyticalAnswer`
- `internal/mcp/codemode/handlers_test.go` — happy paths for each meta-tool.
- `internal/mcp/codemode/policy_test.go` — matchers + actions.

### Integration (`test/integration/codemode/`)

- `TestE2E_CodeMode_OptInAndHappyPath`
- `TestE2E_CodeMode_TwoToolCalls_SaveTokens`
- `TestE2E_CodeMode_PolicyDeny_BlocksToolFromInsideSandbox`
- `TestE2E_CodeMode_ApprovalSuspension_AndResume`
- `TestE2E_CodeMode_PerTenantStubs_Isolated`
- `TestE2E_CodeMode_AuditEnvelope_Complete` — every span/audit attribute present on every tool dispatched from inside the sandbox.
- `TestE2E_CodeMode_PlaygroundFlow` — Playwright; covers editor + run + result.
- `TestE2E_CodeMode_SkillPackSnippet_OneClickInsert`

### Smoke

`scripts/smoke/phase-13.5.sh`:
- Initialize a session with `_meta.portico.code_mode = {enabled: true}` → 200.
- `tools/list` → returns four `mcp.*` tools, not the regular catalog.
- `tools/call mcp.listToolFiles` → returns non-empty array of `.pyi` paths.
- `tools/call mcp.readToolFile` on first path → 200 + body looks like `.pyi`.
- `tools/call mcp.executeToolCode` with a snippet calling one tool → 200 + result present.
- Same with a snippet calling a `requires_approval` tool → 200 + `status: approval_required` + token returned.
- Approve via REST → resume → 200 + final result.
- A snippet `load("os", "system")` → 400 + `code_mode.unsafe_call`.
- A snippet that runs for >5 s with budget=2s → 400 + `code_mode.budget_exceeded`.
- `GET /api/code-mode/executions?session=…` → 200 + recent executions.
- A snippet with tenant A's session ID cannot access a stub authored under tenant B (cross-tenant isolation smoke).

OK ≥ 12 by phase close, FAIL = 0.

### Coverage gates

- `internal/mcp/codemode/`: ≥ 85% overall.
- `internal/mcp/codemode/runtime/`: ≥ 90% (security-critical).
- `internal/mcp/codemode/catalog/`: ≥ 85%.

## Common pitfalls

- **Letting Starlark see the `Load` callback.** A misconfigured thread that accepts `load("…")` becomes an arbitrary-module-loader. Tests assert `thread.Load == nil` and that `load(...)` is rejected at parse time. Reviewers look for any `*starlark.Thread` constructor that does not zero `Load`.
- **Forgetting to thread tenant/JWT through bindings.** Tool callables must take a `context.Context` from the outer execution span, not synthesise one. Otherwise the inner dispatcher loses tenant identity and the audit/policy stack misbehaves. The binding wrapper template (`runtime/bindings.go::bindToolFunc`) is the only correct way; all bindings flow through it.
- **Caching tool definitions across snapshot rotations.** The projector is pure given a snapshot, but the *snapshot* changes when the catalog drifts. A code-mode session pinned to snapshot X cannot see tools added to snapshot Y. The runtime must refuse a resume after snapshot drift (`code_mode.snapshot_drifted`).
- **Print-output secret leakage.** `print()` from inside the sandbox can echo arguments. Output is routed through the same redactor that handles audit payloads (Phase 5). Tests inject a known-shape secret and assert it is redacted in both the user-visible response and the audit row.
- **Continuation-replay determinism.** Replaying a code snippet against cached results assumes Starlark is deterministic for the bound globals. If any binding (e.g. `time.now()`) varies across replay, the call ordering may change. We expose only deterministic bindings; `time.now` returns the *original* execution timestamp during replay, not the wall clock.
- **Approval-suspended executions hanging forever.** Continuations have a 24 h TTL (configurable). After expiry, resume fails with `code_mode.continuation_expired`. A background sweeper deletes expired rows.
- **Tool name collisions in Starlark namespaces.** Bifrost projects each server into its own Python module (`github.list_issues`). Two MCP servers with the same name would shadow each other. Our snapshot model already namespaces by server; the projector preserves that. Tests assert.
- **MCP method namespace pollution.** The four meta-tools live under `mcp.*`. Other code paths must not register tools with that prefix. §13 forbidden practices addition: "Registering a tool whose namespaced name begins with `mcp.` from anywhere except `internal/mcp/codemode/`."
- **Performance of large-snapshot projection.** A 500-tool snapshot rendered as 500 individual `.pyi` files is slow if regenerated per request. The projector caches by snapshot ID inside `internal/mcp/codemode/catalog/cache.go`. Cache eviction is snapshot-rotation driven.
- **Sandbox memory bombs.** Starlark itself does not have a heap limit. We bound output buffer + step count; we additionally watchdog the goroutine and kill at the wall-clock deadline. A snippet that holds large lists in memory consumes within budget because the step limit fires first on the algorithmic explosion (1 M list ops at 100K-step budget yields no room for actual work).
- **Skill Pack snippet trust.** Operator-authored snippets shipping in a Skill Pack are trusted to the same level the Skill itself is. The Code Mode editor labels their origin; the `/skills/<id>` page surfaces the snippet's audit history.

## Out of scope

- Full Python (not Starlark).
- Multi-language sandbox (no JS/WASM/Lua).
- Persistent sandbox state across calls.
- GPU / network / filesystem access from the sandbox.
- Long-running background tasks inside the sandbox (no `async`, no `spawn`).
- Auto-generated `.pyi` stubs for *Skills* (Skills will surface via the regular snapshot rendering, not as separate stub files — they are already named-and-versioned tools).
- Bifrost product integration (no shared admin surface; no remote attestation against Bifrost's runtime).

## Done definition

1. All acceptance criteria pass.
2. `make preflight` green; `scripts/smoke/phase-13.5.sh` shows OK ≥ 12, FAIL = 0; prior smokes unaffected.
3. Coverage gates met.
4. Docs site gains `/docs/concepts/code-mode`, `/docs/concepts/code-mode-savings`, `/docs/how-to/use-code-mode`.
5. `AGENTS.md` §13 forbidden practices updated:
   - "Registering a tool whose namespaced name begins with `mcp.` from anywhere except `internal/mcp/codemode/`."
   - "Exposing host-side I/O (file, network, subprocess) to Starlark from inside the Code Mode runtime."
6. RFC-001 updated with a Code Mode section documenting the four meta-tools, the sandbox guarantees, and the approval-suspension protocol.
7. `docs/plans/README.md` index lists Phase 13.5 with status.

## Hand-off to Phase 14 / 15.5

Code Mode is a presentation layer over the same MCP catalog and the same governance envelope. Phase 14's substrate refactor sees Code Mode as another set of methods on the MCP listener — no special-casing required. Phase 15.5's semantic cache extends naturally to Code Mode tool calls (each call inside the sandbox is cacheable by its arguments + snapshot id). The Virtual Keys + hierarchical budgets in Phase 15.5 apply uniformly to a Code Mode execution because every tool call inside it goes through the regular dispatcher.
