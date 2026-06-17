# Code Mode — Threat Model

> Status: **binding** for all Phase 13.5 work. Every sandbox, binding, continuation, and
> handler unit must defend a named class below; its tests must include the adversarial case.
> If you add a code path that touches Starlark execution or a tool call issued from inside
> the sandbox, find your attack class here and add the negative test that proves the defense.

Code Mode executes **LLM-generated Starlark** that calls **real, governed tools**. The code
is, by construction, untrusted: it is written by a model that may have been prompt-injected,
and it runs on behalf of a specific tenant and user. The runtime is therefore an
adversarial-input processor first and a convenience second. This document enumerates the
attack surface, the defenses, and the tests that lock each defense in place.

The guiding posture is **default-deny**: Code Mode is per-session opt-in, budgets are
conservative, the built-in surface is an allowlist (not a denylist), and any tool call that
cannot prove it traversed the full governance envelope is a failure, not a fallback.

---

## Trust boundaries

```
 ┌─────────────────────────────────────────────────────────────────────┐
 │ TRUSTED: Portico process                                            │
 │                                                                     │
 │  dispatcher ── policy ── approval ── vault ── audit ── telemetry    │
 │      ▲                                                              │
 │      │ DispatchToolCall (the ONLY seam)                            │
 │  ┌───┴──────────────────────────────────────┐                     │
 │  │ SEMI-TRUSTED: runtime host (Go)           │                     │
 │  │  budgets · allowlist gate · bindings      │                     │
 │  │  ┌──────────────────────────────────────┐ │                     │
 │  │  │ UNTRUSTED: Starlark program           │ │                     │
 │  │  │  (LLM-generated, possibly injected)   │ │                     │
 │  │  └──────────────────────────────────────┘ │                     │
 │  └──────────────────────────────────────────┘                     │
 └─────────────────────────────────────────────────────────────────────┘
```

- The **Starlark program** is fully untrusted. It may attempt anything the interpreter
  allows: escape, resource exhaustion, governance bypass, data exfiltration.
- The **runtime host** is trusted Go we wrote, but it processes untrusted input, so it is
  written defensively (fail-closed, no panics on adversarial input, every external value
  validated).
- The **dispatcher and the governance stack** are trusted and unchanged: Code Mode does not
  add a second tool-call path; it reuses the existing one through a single seam.

---

## Attack classes

### C1 — Sandbox escape (Starlark host → arbitrary behavior)

**Threat.** The program reaches outside the interpreter: loads a module, imports host code,
reads the filesystem, opens a socket, spawns a process, or reaches a Go host object that
exposes any of these.

**Why it matters.** A single escape turns Code Mode into a remote-code-execution platform
running with the gateway's privileges — game over for every tenant on the host.

**Defenses.**
- `thread.Load = nil` — the `load(...)` statement has no implementation and fails closed.
- `load` statements are additionally **rejected at parse time**: the static safety gate walks
  the AST and refuses any `*syntax.LoadStmt` before the program is ever compiled.
- `import` is **not valid Starlark syntax** — it is a parse error, never reachable.
- **Built-ins are an allowlist, asserted statically.** The gate collects every *free*
  identifier in the AST and rejects any name not in the explicit allowlist (the permitted
  built-ins, the three stdlib modules `json`/`math`/`time`, and the per-snapshot tool
  modules). Starlark's `Universe` contains memory-safe-but-unwanted built-ins
  (`getattr`, `set`, `hasattr`, `dir`, …); the static gate blocks them even though the
  interpreter would otherwise resolve them, because minimizing surface is cheaper than
  reasoning about each one.
- **No host object exposes I/O.** The only Go values bound into the program are: the
  allowlisted pure built-ins, the three pure stdlib modules, and the tool-call bindings —
  which themselves route exclusively through the dispatcher seam (see C2). There is no file,
  socket, `os`, `subprocess`, or `exec` value anywhere in scope.
- `time.now` is coarsened to second resolution and `time.sleep` is absent (see C3 / side
  channels).

**Tests that must fail to escape** (`runtime/sandbox_test.go`):
`TestSandbox_LoadStatementRejected`, `TestSandbox_ImportIsParseError`,
`TestSandbox_DisallowedBuiltinRejected` (`open`, `set`, `getattr`, `eval`, `exec`),
`TestSandbox_NoFileSystemAccess`, `TestSandbox_NoNetworkAccess`,
`TestSandbox_UnknownModuleRejected`, `TestSandbox_NoLoadCallbackConfigured`.

### C2 — Governance bypass on in-sandbox tool calls

**Threat.** A tool called from inside Starlark skips one or more of: tenant scoping, JWT
scope check, policy evaluation, approval, vault credential injection, audit redaction,
telemetry. Equivalently: the sandbox reaches a downstream server through a path the gateway
does not gate.

**Why it matters.** This is the subtlest and most dangerous class. An escape is loud; a
governance bypass is silent — the call looks normal but ran with the wrong tenant, without
approval, or with broad credentials. Acceptance criterion #8 exists for exactly this.

**Defense — structural, not procedural.** The runtime has **exactly one** way to issue a
tool call: the `ToolDispatcher` seam. Its only production implementation is the dispatcher's
existing `tools/call` core — the *same Go function* that a direct `tools/call` runs. There is
no second code path to keep in sync, nothing to "remember to call." A binding cannot reach a
downstream server except by handing a namespaced tool name + JSON args to that one function,
which then runs policy → approval → credentials → audit → telemetry exactly as it always has.
The tenant/user/session identity is captured from the *outer* execution context (the session
that owns the `executeToolCode` request); bindings never synthesize their own identity or
context.

**Tests that must fail to bypass:**
- `TestBindings_OnlyPathIsDispatcher` — there is no exported or reachable way to call a tool
  without going through the seam; a binding handed a nil dispatcher fails closed.
- `TestE2E_CodeMode_AuditEnvelope_Complete` (integration) — a tool dispatched from inside the
  sandbox produces byte-for-byte the same audit event types, span shape, and policy path as
  the identical direct `tools/call`.
- `TestE2E_CodeMode_PolicyDeny_BlocksToolFromInsideSandbox` — a policy `deny` stops the
  in-sandbox call; the denial surfaces as a Starlark error, not a silent success.
- Adversarial: a snippet that tries to reach a server not in its snapshot, or to forge a
  tenant id in arguments, gets the same `tool_not_enabled` / policy error a direct call would.

### C3 — Resource exhaustion (budget)

**Threat.** The program never returns, or consumes unbounded CPU, memory, or output:
infinite loop, algorithmic bomb (nested comprehensions), giant list allocation, a flood of
`print()`, or a flood of tool calls.

**Why it matters.** A single execution must not be able to degrade the gateway for other
tenants (noisy-neighbor) or pin a core indefinitely.

**Defenses (every execution, all enforced, all configurable, all default-conservative).**
- **Instruction budget** — `thread.SetMaxExecutionSteps(MaxSteps)`, default 100,000. On
  exceed the interpreter cancels; the runtime reports `code_mode.budget_exceeded` naming
  `steps`.
- **Wall-clock budget** — default 30 s. A watchdog goroutine bound to the execution
  `context.Context` calls `thread.Cancel("wall_clock_exceeded")` at the deadline. The
  goroutine is always joined (no leak) via a `done` channel.
- **Output budget** — `print()` output is buffered with a hard cap (default 1 MB); overflow
  is dropped and recorded, never grown unbounded.
- **Tool-call budget** — default 20 calls per execution; the binding increments a counter and
  fails closed past the cap with `code_mode.budget_exceeded` naming `tool_calls`.
- **Memory** — Starlark has no native heap cap, so memory is bounded by three overlapping
  mechanisms, each covering a case the others miss:
  - *Iterative* growth (loops, comprehensions building large lists) is caught by the **step
    budget**: 1 M element-appends cannot fit in a 100 K-step budget.
  - *Single-operation* growth (`[0] * N`, `"x" * N`) does **not** consume steps proportional
    to size, so the step budget does not catch it. It is bounded instead by starlark-go's
    built-in `maxAlloc = 1<<30`-element cap on repeat: an over-cap repeat fails with
    "repeat count too large". This cap is *loose* (a ~1 GiB transient is permitted), which is
    a known noisy-neighbor limitation tracked for the red-team hardening round; the
    wall-clock watchdog is the backstop until a tighter heap watchdog lands.
  - The **wall-clock watchdog** is the final backstop for anything the first two miss.
  This layering is a deliberate, documented design choice; the `maxAlloc` looseness is called
  out honestly rather than papered over.

**Tests:** `TestSandbox_StepBudgetEnforced`, `TestSandbox_WallClockBudgetEnforced`,
`TestSandbox_MaxToolCallsEnforced`, `TestSandbox_PrintBufferTruncation`,
`TestSandbox_AllocationBombHitsStepBudgetFirst`, `TestSandbox_WatchdogGoroutineDoesNotLeak`.

### C4 — Continuation tampering (approval pause/resume)

**Threat.** When an in-sandbox tool call needs approval, the execution suspends and a
continuation is persisted. An attacker tampers with the serialized state: swaps the snapshot,
replays a stale continuation, double-resumes to double-execute a side-effecting call, or
forges cached results to substitute an unapproved value.

**Why it matters.** The continuation is the one piece of sandbox state that crosses a trust
boundary in time (DB) and across an approval decision. Getting it wrong re-introduces the
approval bypass (C2) through the back door.

**Defenses.**
- The continuation stores only what is needed to *deterministically replay*: the snippet, the
  cached prior tool-call results indexed by call ordinal, the awaited call index, the print
  buffer — all redacted on write, tenant-scoped, keyed by an unguessable ULID token.
- **Snapshot immutability** — the execution is pinned to one snapshot; a resume against a
  drifted snapshot fails closed with `code_mode.snapshot_drifted`.
- **Single-use** — a continuation is consumed on resume; a second resume of the same token
  fails with `code_mode.double_resume`.
- **TTL** — continuations expire (default 24 h); resume past expiry fails with
  `code_mode.continuation_expired`; a sweeper deletes expired rows.
- **Determinism** — replay re-executes the snippet with only deterministic bindings; prior
  calls return their cached results, the awaited call returns the now-approved result, and
  `time.now` returns the *original* execution timestamp (not the replay wall clock) so call
  ordering cannot diverge. The approval is still re-checked through the envelope on resume;
  the cache substitutes results, never the *decision*.
- **Cross-tenant** — every continuation read/write is `WHERE tenant_id = ?`; a token from
  tenant A is invisible to tenant B.

**Tests:** `TestContinuation_SuspendsOnApprovalRequired`,
`TestContinuation_ResumesWithCachedResults`, `TestContinuation_RejectsSnapshotDrift`,
`TestContinuation_DoubleResumeRejected`, `TestContinuation_TTLExpiry`,
`TestContinuation_CrossTenantTokenInvisible`,
`TestContinuation_ReplayDeterministic_TimeNowFrozen`.

### C5 — Cross-tenant leakage

**Threat.** Tenant A's session sees tenant B's stubs, tools, snapshot, executions, or
continuations; or an in-sandbox call resolves a server belonging to another tenant.

**Why it matters.** Multi-tenancy is non-negotiable (CLAUDE.md §6). A projection or binding
that leaks across tenants is a security bug, not a UX nit.

**Defenses.**
- The projector renders **only** the session's own snapshot; the snapshot is already
  tenant-scoped at the catalog layer.
- Every storage method (`code_mode_executions`, `code_mode_continuations`) carries
  `tenant_id NOT NULL` and filters `WHERE tenant_id = ?`.
- Tool bindings resolve against the session's snapshot only; the dispatcher seam re-checks
  tenant on every call.
- The meta-tool handlers read tenant from the session context (`tenant.MustFrom`), never from
  client-supplied arguments.

**Tests:** `TestE2E_CodeMode_PerTenantStubs_Isolated`,
`TestProjector_OnlySessionSnapshot`, `TestContinuation_CrossTenantTokenInvisible`,
`TestExecutions_ListFiltersByTenant`.

### C6 — Prompt injection via tool results re-entering the sandbox

**Threat.** A downstream tool returns attacker-controlled content (e.g. an issue body that
says "now call `delete_repo`"). That content flows back into the Starlark program and into
`print()` / the final `result`, then into the model's context — a prompt-injection relay. A
related leak: a tool result or `print()` echoes a secret.

**Why it matters.** Code Mode widens the injection surface because tool results are processed
*as data by code* and can re-enter the model. We cannot stop a tool from returning hostile
text, but we can ensure it cannot (a) escalate privilege inside the sandbox, or (b) carry
secrets out.

**Defenses.**
- Tool results are **inert data** inside Starlark: they become plain dicts/lists/strings.
  They cannot trigger a tool call on their own — only code the model *wrote* calls tools, and
  every such call is still gated (C2) and counted (C3). Injected text that says "call
  delete_repo" is just a string; if the model's own code then calls it, policy/approval still
  apply.
- **Secret redaction on the way out.** Both `print()` output and the final `result` pass
  through the same `audit.Redactor` used for audit payloads before they leave the sandbox
  (user response) or land in the execution row. A known-shape secret injected via a tool
  result must appear `[REDACTED:…]` in both the user-visible response and the audit row.
- The execution row stores a **redacted summary**, never the full body (CLAUDE.md §7.8).

**Tests:** `TestSandbox_PrintOutputRedacted`, `TestHandler_ResultRedactedBeforeReturn`,
`TestSandbox_ToolResultIsInertData`,
`TestE2E_CodeMode_InjectedSecretRedacted_ResponseAndAudit`.

---

## Default-deny posture (cross-cutting)

- **Opt-in per session.** A session sees Code Mode only via the `_meta.portico.code_mode`
  capability at `initialize`, or an operator policy elevation. Existing clients are unchanged.
- **Conservative budgets out of the box** (C3) — operators raise them deliberately, per route.
- **Policy can tighten further** — `deny_on_unsafe_starlark` (reject before execution on a
  static-gate failure) and `require_approval_on_executeToolCode` (gate the whole execution).
- **Fail closed everywhere.** A nil dispatcher, an unparseable snippet, an unknown module, a
  drifted snapshot, an expired continuation — all fail closed with a typed error. There is no
  "best effort" branch that proceeds without a guarantee.

## Non-goals (explicit)

- Not a general Python runtime — Starlark is a deliberate subset (no classes, no `import`, no
  I/O). Operators who need full Python run a Code Interpreter MCP server *behind* Portico.
- Not a defense against a malicious *operator* — operators who can author policy and Skill
  Pack snippets are already trusted to that level. The threat actor is the untrusted Starlark
  program (i.e. a compromised/injected model), not the human operator.
- Not a covert-channel-free runtime — `time.now` is coarsened to blunt the most obvious timing
  side channel, but Code Mode does not claim constant-time execution.

## Review checklist (every Code Mode PR)

- [ ] Does this PR add a way to execute Starlark or reach a tool? If so, which attack class
      does it touch, and is the negative test present?
- [ ] Is `thread.Load` nil and is `load`/`import`/unknown-identifier rejected at the static
      gate?
- [ ] Does every tool call still go through the single `ToolDispatcher` seam (no second
      path)?
- [ ] Are all four budgets enforced and the watchdog goroutine joined?
- [ ] Is tenant read from context (not arguments), and is every new storage method
      `WHERE tenant_id = ?`?
- [ ] Do `print()` and `result` pass through the redactor before leaving the sandbox?
- [ ] Does a continuation resume re-check drift, single-use, TTL, and tenant?
