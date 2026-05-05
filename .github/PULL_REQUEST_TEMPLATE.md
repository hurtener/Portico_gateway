<!--
Thanks for contributing to Portico. Before opening this PR, please skim AGENTS.md
(or CLAUDE.md — same content) and ensure the relevant checklist items below pass.
-->

## Summary

<!-- 1–3 sentences. What problem does this solve and what's the approach? -->

## Phase / RFC reference

- [ ] This change implements part of `docs/plans/phase-N-*.md` — phase: <!-- N -->
- [ ] This change updates the RFC (`RFC-001-Portico.md`) — section: <!-- § -->
- [ ] This change is unrelated to the phased plan (explain why)

## Checklist

- [ ] All new packages have godoc package comments.
- [ ] Every storage method touching tenant-scoped tables takes a `tenantID` and filters by it.
- [ ] No secrets in code or fixtures; vault is used for any credential.
- [ ] `slog` used for logs; no `fmt.Println` or `log.Println` in production code paths.
- [ ] `context.Context` flows through every public function that does I/O or work that should be cancellable.
- [ ] Goroutines are cancellable via `ctx` and are joined on shutdown (no leaks).
- [ ] Tests added/updated; named per the relevant phase plan if applicable.
- [ ] `go test -race ./...` passes locally.
- [ ] `golangci-lint run` passes locally (or notes the disabled lints in the description).
- [ ] If this changes a multi-tenant boundary, an integration test asserts cross-tenant isolation.
- [ ] If this introduces a new MCP message or REST endpoint, both are documented in the RFC or the relevant plan.
- [ ] If this changes `portico.yaml` schema, the change is backward-compatible OR the RFC is updated.

## Test plan

<!-- What did you run? What's the output? Paste the smallest evidence (test name, brief result). -->

## Out of scope

<!-- Anything tempting but deferred. Helps reviewers stay focused. -->
