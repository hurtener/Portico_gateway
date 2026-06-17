# Phase 20 — Productization (workflow-driven verification pass)

> Self-contained plan. Runs **after Phases 13.5–19 are built and before Phase 12 (launch)**.
> This is the pre-launch quality gate: prove that everything we *claim* is actually true,
> that each feature delivers real value, and that every Console page's UX is accurate,
> correct, and consistent. It is executed primarily through the orchestrator's **multi-agent
> Workflow** capability (fan-out verification + adversarial checks), not hand-written one-off
> tests.

## Goal

Before we ship, take the whole product and **adversarially verify it end to end**. Phases 0–19
build features under their own acceptance criteria, but those criteria are written by the same
mind that builds the feature. Phase 20 is the independent pass: confirm the binary, the REST/MCP
surfaces, the Console, and the docs all tell the *same, true* story — and that a real operator
gets value, not just green tests.

## Why this phase exists

- **Claims drift from reality.** READMEs, page copy, plan acceptance notes, and marketing
  positioning accumulate statements ("supports N providers", "redacted before persistence",
  "tenant-isolated") that were true when written and may have silently regressed. Phase 20
  re-checks each load-bearing claim against the running binary.
- **Green ≠ valuable.** A page can render, pass svelte-check, and have a Playwright spec while
  still being confusing, mis-labelled, dead-ended, or not actually wired to the thing it claims
  to show. Phase 20 evaluates the *operator experience*, page by page.
- **Cross-feature consistency.** Naming, empty states, error envelopes, scope gating, and design
  tokens must be consistent across the surface a launch exposes. One pass over the whole thing
  catches what per-phase work cannot.

## How it runs — Workflow orchestration

The orchestrator drives this phase with the **Workflow** tool (multi-agent fan-out + adversarial
verify), not solo. Representative shapes:

- **Claim audit.** Enumerate every load-bearing claim (README, page copy, plan acceptance
  criteria, positioning docs, CLAUDE.md normatives). Fan out one verifier per claim that checks
  it against the live binary / source, returning {claim, location, verdict, evidence}. Adversarial
  second pass tries to *refute* each "true" verdict. Output: a claim-truth matrix; every false or
  unverifiable claim becomes a fix.
- **Page-by-page UX audit.** One agent per Console route boots `./bin/portico dev`, drives the
  page (Playwright), and scores: does every advertised affordance work, is copy accurate, are
  empty/loading/error states correct, is it reachable from nav, does it match the design tokens,
  is it wired to real data (not a stub). Findings verified adversarially before they count.
- **Value/coherence review.** A judge panel asks, per feature area, "would a real operator get
  the value we claim, and is the flow coherent?" — surfacing dead-ends, missing CTAs, and
  governance gaps a single builder misses.
- **Completeness critic.** A final agent asks "what surface did we NOT audit?" and queues it.

## Deliverables

1. A **claim-truth matrix** (every load-bearing claim → verified | fixed | retracted), checked in
   under `docs/` so launch copy is provably accurate.
2. A **per-page UX audit report** with a fix list; every Console route passes the rubric (works,
   accurate copy, all states, reachable, tokens, real data).
3. **Fixes** for every confirmed defect (copy, wiring, UX gaps, claim corrections) — landed as
   normal PRs.
4. **Regression locks** for the highest-value findings (smoke / Playwright / handler tests) so the
   defect class can't return.
5. A short **launch-readiness summary** the Phase 12 work consumes.

## Acceptance criteria

1. Every load-bearing claim is verified-true or corrected/retracted; no known false statement
   reaches launch copy.
2. Every Console route passes the UX rubric (works · accurate copy · empty/loading/error states ·
   reachable from nav · design-token compliant · wired to real data).
3. The product's stated value per feature area is demonstrated against the live binary, not just
   asserted.
4. All confirmed defects are fixed; the top findings have regression locks.
5. `make preflight` + the full frontend e2e suite green; no FAIL on `main`.

## Out of scope

- New features. Phase 20 verifies and fixes; net-new capability belongs to its own phase.
- Launch mechanics (release artifacts, onboarding wizard, docs site) — that is **Phase 12**, which
  runs *after* this phase and consumes its readiness summary.

## Sequencing

Build order: Phase 13 ✅ → 13.5 → 14 → 15.5 → 16 → 17 → 18 → 19 → **20 (this phase)** → 12 (launch,
last). Phase 20 cannot start until 13.5–19 are merged, because it audits the whole surface.

## Hand-off to Phase 12

Phase 12 (onboarding/distribution/launch) starts from a product that has been adversarially
verified end to end, with a launch-readiness summary and a clean claim-truth matrix in hand.
