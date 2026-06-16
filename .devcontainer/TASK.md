# Build target — current unit

> **This file is the builder's brief.** A fresh, stateless free-model session reads it
> at the start of EVERY loop iteration. It must be **self-explanatory**: assume the agent
> has no memory of prior runs and no context beyond this file, `CLAUDE.md`, the named phase
> plan, and `git log`. The orchestrator (capable model) rewrites this file per unit before
> launching `./.devcontainer/run.sh`. Keep exactly ONE active unit here at a time.
>
> **Status when this file shipped with the engine PR:** PLACEHOLDER. No live target is set.
> The orchestrator writes the first real unit (below) before the first loop run.

---

## Unit: <short title of the one coherent unit>

**Phase plan:** `docs/plans/phase-<N>-<slug>.md` — read it in full; its acceptance criteria are the definition of done.

**Why this unit (one paragraph, plain language).**
<What this unit is, where it sits in Portico, and why it matters. Written so a low-context
agent understands the intent without reading the whole RFC. Name the user-visible behaviour
that should exist when this is done.>

**In scope (do exactly this).**
- <bullet — the specific package(s)/file(s)/surface to create or change>
- <bullet — the concrete behaviour to implement>
- <bullet — keep it to ONE coherent unit; if it needs more, the orchestrator splits it>

**Out of scope (do NOT touch this iteration).**
- <bullet — adjacent work that belongs to another unit>
- Anything not named in "In scope". No drive-by refactors.

**Reference pattern to copy.**
<Point at an already-landed file that demonstrates the exact pattern for this kind of work,
e.g. "internal/storage/ for the ifaces+factory+registry seam" or
"internal/server/api/servers.go for a tenant-scoped CRUD handler". The breadth a cheap model
produces is only as good as the example it copies — always give it one.>

**Acceptance checks (the agent verifies these before emitting [goal:complete]).**
- [ ] <criterion copied/distilled from the phase plan — concrete and checkable>
- [ ] New/changed HTTP endpoint or MCP method has a smoke check in `scripts/smoke/phase-<N>.sh`
- [ ] Multi-tenant: tenant_id column + `WHERE tenant_id = ?` filtering if a tenant-scoped table is touched
- [ ] Tests exist next to the code and assert real behaviour (not stub constants)

**Gates (run from repo root; all must be green).**
```
make vet
make test            # go test -race ./...
make lint            # golangci-lint v1.64.8
make build           # CGO_ENABLED=0 static binary
make preflight       # if an HTTP/MCP surface changed (boots the binary + smoke)
make frontend-check  # if web/console/ changed
make check-mirror    # AGENTS.md ≡ CLAUDE.md
```

**Stop conditions.**
- All gates green + acceptance checks pass → output `[goal:complete]` on its own line and STOP.
- A gate cannot pass → output `[goal:blocked] <reason with file:line>` and STOP.
- Do NOT commit, push, or open a PR — the orchestrator owns the git surface and verifies independently.

---

### Orchestrator notes (not read by the builder loop; context for the human + capable model)

- The next units in sequence after this one (so the orchestrator knows what TASK.md becomes next):
  `<phase / unit / unit …>`
- Findings from the last adversary pass that must be fed back as the next narrow unit:
  `<file:line — defect — fix direction>`
