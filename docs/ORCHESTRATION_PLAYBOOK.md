# Orchestration playbook — moving Portico forward, one phase at a time

> **Purpose.** A repeatable cadence for advancing Portico with a **hybrid model split**:
> a cheap/free *builder* model does the mechanical volume, and a capable *orchestrator*
> (you) plans, adversarially verifies, fixes the hard parts, and owns the merge gate.
> Portico is large, well-specified, and phase-driven — exactly the kind of project where
> **slow and steady** wins: land one phase (or one coherent sub-unit) fully verified and
> merged before starting the next, and the gateway moves forward reliably without you
> typing every line.

This doc is the **cadence**. The **engine room** — how to stand up the unattended
devcontainer, the stateless loop, model fallback, secret injection, and the long tail of
real-world gotchas — lives in the **`orchestrate-autonomous-build` Claude skill**. Read
that skill before your first run; this playbook assumes it. Don't duplicate the container
mechanics here — link to the skill and keep this focused on the per-phase loop and the
Portico-specific anchors.

---

## The cadence (one cycle per phase, repeat)

Each phase is one trip around this loop. Nothing advances on the builder's say-so — only
on the orchestrator's verified gate.

1. **Orchestrator — plan the phase.**
   Read the design source of truth and the phase plan, settle the scope, and write the
   builder's task prompt. Decompose into **one coherent unit per builder iteration**
   (one module, one surface, one endpoint-set). For a novel or cross-cutting slice, build
   the **first reference unit yourself** so the builder has a correct pattern to copy. Pin
   the gates this phase must pass and write them into the prompt. *Portico anchors below.*

2. **Builder — implement.**
   A fresh, stateless builder iteration takes one unit and implements it in the
   devcontainer, following the framework's own skills/CLIs (never reverse-engineering an
   API from training data). It builds against the gates and stops with a token
   (`[goal:complete]` / `[goal:blocked]`).

3. **Builder — iterate.**
   The loop re-runs fresh iterations until the unit self-reports done: the builder
   re-orients from git state each time, self-corrects against the gates, and recovers from
   crashes/rate-limits via the loop's fallback logic. Keep iterations small so a session
   never grows big enough to wedge on compaction.

4. **Orchestrator — adversary.**
   **Treat `[goal:complete]` as the start of verification, never the end.** Independently:
   run the machine gates per module; **read the actual handler/component bodies** for the
   stub trap (registered surface vs. code that calls the real backend; orphaned dead-code
   helpers); count artifacts against claims; and **live-validate** the surface (it
   renders/responds against fixtures, not just "it compiles"). Record findings with
   `file:line` evidence and a fix direction.

5. **Builder — fix.**
   Feed the findings back as targeted, narrow tasks (one concern at a time). The builder
   is good at fixing a precise, evidenced defect in a known location — far better than at
   open-ended "make it better." Re-run until the findings clear.

6. **Orchestrator — evaluate & final fix.**
   Re-verify the whole unit. Apply the **targeted fixes the cheap model can't reliably
   get** yourself — recurring mechanical errors, subtle contract/wiring bugs, the
   one-line correctness fix that would cost ten cheap iterations. Run the polish passes in
   order (correctness → usability/surface → docs → README) until the unit meets the bar.

7. **Orchestrator — commit + PR + approve + merge.**
   Own the git surface. Branch off `main`, commit the unit with a message that states what
   the builder produced **and** what you fixed on review, open a PR, and merge it as the
   explicit integration step (self-approve/admin-merge is fine for a solo cadence —
   **never push straight to `main`**). The merged history is the durable progress record.

8. **Repeat** for the next unit/phase.

> The shape, in the user's words: *orchestrator plan → builder implement → builder iterate
> → orchestrator adversary → builder fix → orchestrator evaluate + final fix → orchestrator
> commit + PR + approve → repeat.*

---

## Portico-specific anchors

Slot the generic cadence onto this repo's real artifacts and gates.

**Priority chain (when two artifacts disagree).**
`RFC-001-Portico.md` → `docs/plans/phase-*.md` → `AGENTS.md` (≡ `CLAUDE.md`, mirrored
verbatim) → code comments/godoc. Resolve drift by fixing the lower-priority artifact, never
by silent divergence. `make check-mirror` enforces the `AGENTS.md`/`CLAUDE.md` mirror.

**Where the plan lives.** `docs/plans/phase-N-*.md` is the unit of work — each phase has a
goal, deliverables, and acceptance criteria. A phase is done only when its criteria pass.
The design system + reference renders (`docs/Design/design-system.md` + the PNGs) are the
multimodal target for any console/UI work — a multimodal builder can *see* and match them.

**Machine gates (run per module, green or not done).**
`make preflight` is the CI gate — it runs `check-mirror` then `scripts/preflight.sh`, which
**boots the binary and runs HTTP smoke checks against every implemented surface**. Plus
`make test` (`go test -race`), `make vet`, `make lint` (golangci-lint), `make frontend-check`
(`svelte-check` + build), and the secret scan. Don't accept a "gates pass" claim that only
ran the root module — enumerate what was actually checked.

**Live validation (the "renders/responds" proof).**
Portico ships its own operator surfaces — exercise the change through them, don't trust a
standalone serve:
- The **Console** SPA (servers, sessions, resources, prompts, MCP Apps, skills, tenants,
  secrets, policy) — drive it with Playwright (`.playwright-mcp/` is already wired).
- The **Playground** (`/playground`) — run real MCP tool calls / prompts / resource reads
  against the live catalog, with the right-rail correlator (spans, audit, policy decisions,
  drift). Saved cases + one-click replay are a ready-made regression harness for a new
  server / skill / policy edit.
- `scripts/preflight.sh`'s HTTP smoke checks for the northbound/admin endpoints.

A surface that compiles, mounts, and is themed can still be functionally dead (wrong payload
path, missing fixture, blank pane, zero console errors). **Only the live surface proves it.**

**The stub trap, Portico flavor.** A `Register(...)` that doesn't receive its backend
dependency, a handler that returns a hardcoded/empty struct with no client/store call, or
real logic stranded in an unwired helper — all compile and pass a shallow gate. Grep for it;
verify the integration hits the right endpoint/store with the right shape against a
behavioral oracle, not just "a call exists."

---

## Non-negotiables (the discipline that makes it work)

- **Verify, don't trust.** The builder's completion token is an untrusted claim. Advance
  only on *your* machine-checkable gate **plus** a semantic read of the real code **plus**
  a live-validation pass. This is the whole game (skill §7).
- **One coherent unit per iteration.** Cheap models execute well on narrow scope and
  degrade badly on broad scope. Never run a single monolithic "build until done" loop.
- **Escalate the hard slice to yourself.** Novel, cross-cutting, or repeatedly-failing
  work is cheaper for you to do once than to babysit through ten cheap iterations. Build
  the reference unit; clear recurring mechanical errors directly.
- **Follow the framework's skills/tools.** Use the official generators/validators/CLIs;
  forbid the builder from reverse-engineering APIs. Hand-written contract/schema output
  goes stale and breaks the toolchain.
- **Merge deliberately via PR.** The integration step is explicit and orchestrator-owned.
- **Write findings down.** Durable docs with `file:line` evidence (e.g. a phase findings
  note under `docs/plans/`), fed back as targeted builder tasks — not ephemeral chat.

When the bar must be highest, or the cheap loop is too slow for a big mechanical-but-quality
pass, escalate that pass to **capable-model multi-agent workflows** (foundation barrier →
one agent per unit → adversarial critic per unit → final integration gate you re-verify) —
see skill §8. Reserve it for the hard/novel slices and the final quality pass, not cheap
breadth.

---

## See also

- **`orchestrate-autonomous-build` Claude skill** — the full method: container setup,
  stateless loop + model fallback, secret injection, plan decomposition, the
  verify-don't-trust depth, and the distilled gotchas checklist (dual-loop contention,
  write truncation, compaction wedge, per-module verification, stale-render ports, …).
  **This playbook is the Portico-anchored cadence; the skill is the engine room.**
- `RFC-001-Portico.md`, `docs/plans/`, `AGENTS.md` / `CLAUDE.md` — the authoritative chain.
- Proven at scale on the sibling **WorkBridge** project (a 12-server MCP App suite built
  mostly by free models under this exact cadence) — the skill carries that worked example
  throughout.
