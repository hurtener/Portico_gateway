# Portico — Contributor & Agent Normatives

> This file is **binding** for anyone (human or AI) modifying this repository. It is mirrored verbatim in `CLAUDE.md` so Claude Code picks it up automatically. If the two files diverge, the most recent commit timestamp wins; flag the drift in your PR.

If a rule below conflicts with a section of the RFC or a phase plan, the **RFC wins**, then the **phase plan**, then this file. Update whichever artifact is wrong; do not silently ignore the conflict.

---

## 1. What Portico is

Portico is a multi-tenant MCP gateway and Skill runtime, written in Go, shipped as a single static binary. It speaks MCP outward (to AI clients) and inward (to many downstream MCP servers), with a Skill Pack runtime that binds the open Skills spec to specific servers/tools/policies/UI resources.

Three product properties are non-negotiable:

1. **Multi-tenant from V1.** Tenant identity flows through every layer. No code is allowed that assumes a single tenant.
2. **Headless approval flow.** Approvals are emitted via MCP `elicitation/create` or as structured JSON-RPC errors. Portico does not render its own approval UI.
3. **Credentials live behind the gateway.** Agents never receive broad downstream tokens. All credential plumbing goes through the vault and the credential injectors.

If a change you're about to make would weaken any of these three, stop and reach for the RFC instead of the keyboard.

---

## 2. Authoritative sources (in priority order)

1. `RFC-001-Portico.md` — product intent and design decisions.
2. `docs/plans/phase-N-*.md` — implementation specifications for each phase. Acceptance criteria are binding.
3. `docs/plans/README.md` — cross-cutting conventions (a subset of this file plus index).
4. This file (`AGENTS.md` / `CLAUDE.md`) — operational rules for working in the repo.
5. Code comments and godoc — last and least authoritative; if a comment disagrees with the RFC, the RFC wins.

When a phase plan and the RFC drift, the RFC wins. File a follow-up to update the plan.

---

## 3. Repository layout

```
.
├── RFC-001-Portico.md         # design RFC (v3)
├── README.md                   # quickstart + pointers
├── AGENTS.md / CLAUDE.md       # this file (verbatim copies)
├── Makefile                    # canonical build / test / lint commands
├── Dockerfile                  # distroless multi-stage build
├── .github/
│   ├── workflows/ci.yml        # quality gate
│   ├── dependabot.yml
│   ├── CODEOWNERS
│   └── PULL_REQUEST_TEMPLATE.md
├── .golangci.yml               # lint config
├── .markdownlint.yaml
├── .editorconfig
├── .gitignore
├── go.mod / go.sum             # appears in Phase 0
├── cmd/portico/                # main binary, subcommands
├── internal/                   # all production code
│   ├── config/
│   ├── auth/{jwt,tenant,scope}/
│   ├── storage/{ifaces,sqlite}/
│   ├── secrets/                # vault, oauth, injectors
│   ├── policy/                 # engine, approval flow
│   ├── audit/
│   ├── mcp/{protocol,northbound,southbound}/
│   ├── registry/               # MCP server registry
│   ├── runtime/{process,session}/
│   ├── catalog/{namespace,resolver,snapshots}/
│   ├── skills/{manifest,source,loader,runtime}/
│   ├── apps/                   # ui:// indexer + CSP
│   ├── telemetry/              # slog + OTel
│   └── server/{api,mcpgw,ui}/
├── web/console/                # embedded htmx + Templ UI
├── examples/
│   ├── servers/mock/           # in-process + standalone mock MCP servers
│   ├── skills/                 # 4 reference Skill Packs
│   └── *.yaml                  # example configs
├── test/integration/
└── docs/
    ├── rfc/                    # future home of merged RFCs
    └── plans/                  # phase implementation plans
```

Anything that doesn't have a home above is wrong. If you need a new top-level directory, propose it in the RFC first.

---

## 4. Build, test, lint, run

All of these target the canonical commands. CI runs the same things.

```bash
# Build the binary (CGo-free, single static binary)
make build

# Run the test suite with the race detector
make test

# Static analysis
make vet
make lint            # requires golangci-lint v1.61+

# Live preflight: build, boot dev server, run HTTP smoke tests, tear down.
# This is the SAME gate the pre-commit hook and CI enforce.
make preflight

# Install the git hooks (one-time, per-clone)
make install-hooks

# Run the dev server (binds 127.0.0.1:8080, dev tenant, no JWT required)
./bin/portico dev

# Run with a real config
./bin/portico serve --config portico.yaml

# Validate config / skills
./bin/portico validate --config portico.yaml
./bin/portico validate-skills ./examples/skills/...

# Vault CLI (Phase 5)
./bin/portico vault put|get|delete|list|rotate-key

# Inspect a session offline (Phase 6)
./bin/portico inspect-session <session_id> --output json
```

Coverage targets per phase plan are non-negotiable. If a PR drops coverage below the target for a touched package, it is rejected.

### 4.1 Preflight gate — non-negotiable

Static checks (vet, lint, tests) catch a lot but **not** "the binary boots and the HTTP surface still works." Portico's pre-commit hook and CI both run a live preflight that:

1. Builds `./bin/portico` (no-op if `go.mod` absent).
2. Boots `./bin/portico dev` on `127.0.0.1:18080` with a temp data dir.
3. Waits for `/healthz` to return 200.
4. Runs each `scripts/smoke/phase-N.sh` against the running server.
5. Tears down (graceful TERM, then KILL, then cleanup).

Each phase smoke script auto-skips its surface if the endpoint returns 404/405/501 — so the gate works at every phase: the surfaces that exist must work, the ones that don't yet are fine. **This means: when you ship Phase N, the corresponding `scripts/smoke/phase-N.sh` must already pass before you commit, because pre-commit will run it.**

When you add a feature, extend the relevant `scripts/smoke/phase-N.sh` so the new surface is covered. PRs that introduce a new endpoint without a smoke check are rejected.

To install the pre-commit hook locally:
```bash
make install-hooks
```

To bypass in an actual emergency (e.g. CI is the source of truth and your local box can't run `make build`):
```bash
PORTICO_PREFLIGHT_SKIP=1 git commit -m '...'
```
The PR description must justify the skip. CI still runs the gate; an emergency local skip never reaches `main`.

---

## 5. Code conventions (Go)

### Language and tooling

- **Go 1.22+.** No earlier.
- **Module path:** `github.com/hurtener/Portico_gateway`.
- **CGo is forbidden.** `CGO_ENABLED=0` is enforced in CI build. SQLite uses `modernc.org/sqlite`.
- **Static binary.** `go build -ldflags='-s -w'`. Verified by CI on Linux.

### Style

- `gofmt -s` clean. CI fails otherwise.
- `goimports` with local prefix `github.com/hurtener/Portico_gateway`.
- All exported identifiers documented with godoc comments. Package-level doc comment in every package.
- File naming: lowercase, underscore-separated, no `util.go` / `helpers.go` (find a real name).
- One package per directory. Sub-packages are nested directories.

### Errors

- Wrap with `fmt.Errorf("context: %w", err)`. Never bare-return upstream errors that originated below.
- Sentinel errors (`var ErrFoo = errors.New("...")`) for cases callers compare against. Use `errors.Is` / `errors.As`.
- `errcheck` and `errorlint` are part of `golangci-lint`. Don't suppress without a one-line `//nolint:` comment **with reason**.
- **Never** use `panic` in production code paths except for "this is impossible by construction" cases. A failed `tenant.MustFrom(ctx)` in a handler that requires auth is acceptable; nothing else is.

### Context

- `context.Context` is the **first parameter** of every function that does I/O, waits, or wants to be cancellable. Never store it in a struct.
- Pass `ctx` through; never call `context.Background()` inside business code unless explicitly bridging an unmanaged async boundary, and document why.
- Honor `ctx.Err()` between long phases of work.

### Logging

- **One logger:** `log/slog`. JSON handler in production, text handler in dev.
- Loggers carry these attributes when present: `tenant_id`, `request_id`, `trace_id`, `span_id`, `session_id`, `server_id`, `tool`. Build a request-scoped child via `logger.With(...)` once per request.
- Severity:
  - `Debug` — useful only when debugging.
  - `Info` — lifecycle events worth telling an operator (process started, server registered, snapshot created).
  - `Warn` — unexpected but recovered (downstream timeout but retried, fsnotify burst debounced).
  - `Error` — the request/operation failed.
- **Never log secrets.** Don't log raw tool arguments or results — they routinely contain secrets. Pass through the audit redactor (Phase 5) for anything sensitive.
- Don't `fmt.Println`, `log.Print*`, or write to `os.Stdout` directly outside of CLI command output.

### Concurrency

- Goroutines started by long-lived components (supervisor, drift detector, audit batcher, OTel exporter, hot-reload watcher) **must** be cancellable by a `ctx` and joined on shutdown.
- Bounded channels with explicit drop policies on backpressure. Default: drop-oldest, emit `audit.dropped` event on first drop in a window.
- `sync.Mutex` is the default. Use `sync.RWMutex` only when contention is measured, not assumed.
- No `goto`. No `runtime.Goexit`. No global state mutation outside `init` and (registered) metric definitions.

### Tests

- Unit tests next to the source file: `foo.go` ↔ `foo_test.go`.
- Integration tests under `test/integration/`, package-named per area.
- `t.TempDir()` for any filesystem fixture. Never write to the working tree from tests.
- Mock MCP servers come from `examples/servers/mock/` (in-process) and `examples/servers/mock/cmd/mockmcp/` (standalone binary). Don't roll new mocks per package.
- Test names: `TestXxx_Behavior_Scenario`. Examples: `TestSupervisor_AcquireStartsProcess`, `TestEvaluate_DenyByList`.
- Integration tests beginning with `TestE2E_` are required where listed in phase plans.
- `go test -race ./...` is the gate.
- Skipped tests: `t.Skip("reason: <one line>")`. CI fails on a Skip with reason ending `TODO`.

### Linting

- Don't add `//nolint:` without a comment explaining the reason in the same line. PR reviewers may push back.
- Prefer fixing the root cause over silencing.
- New linters added to `.golangci.yml` only via PR with rationale.

---

## 6. Multi-tenancy — non-negotiable rules

These rules are integrity-critical. A violation is a security bug, not a style nit.

1. **Every tenant-scoped table has a `tenant_id NOT NULL` column.** No exceptions. The list of tenant-scoped tables is in `docs/plans/phase-0-skeleton-tenant-foundation.md` SQL section. New tables you add: ask "is this tenant-scoped?" — if yes, the column is required.
2. **Every storage method that touches a tenant-scoped table takes `tenantID string` and filters with `WHERE tenant_id = ?`.** No "current tenant from a global." No "fetch all then filter in Go."
3. **Tenant context flows from the JWT through the request `context.Context`.** Read it via `tenant.MustFrom(ctx)` in handlers; pass `tenantID` into storage explicitly.
4. **Process supervisor honors runtime mode.** `per_tenant`, `per_user`, and `per_session` modes produce isolated processes. The supervisor's `InstanceKey` carries `TenantID`; it is wrong to drop or default it.
5. **Vault is keyed by `(tenant, name)`.** Cross-tenant reads are impossible by API construction. Don't add a "global secret" path.
6. **Audit events carry `tenant_id`.** The audit query API filters by tenant by default; admin scope can pass an explicit `tenant_id=*` only if the JWT has the `admin` scope.
7. **Cross-tenant isolation has at least one integration test** for any code path that touches multiple tenants. If you add such a path, add the test.

If a change cannot satisfy these without contortion, the design is wrong — propose a fix in the RFC or the phase plan first.

---

## 7. Security — non-negotiable rules

1. **JWT validation: asymmetric algorithms only.** The validator allowlist is RS256/RS384/RS512/ES256/ES384/ES512. **Never** add HS\* or `none`.
2. **Vault master key from `PORTICO_VAULT_KEY` only.** Never hardcode keys, even in tests. Tests use fixtures from `internal/secrets/testdata/` with a documented dummy key.
3. **No credential passthrough.** The default for OAuth flows is token exchange (RFC 8693). Passthrough requires explicit `auth.passthrough: true` AND emits `credential.passthrough` audit events. Don't add passthrough without that scaffolding.
4. **Approval bypass is not a feature.** A tool flagged `requires_approval` always goes through the approval flow. Caching approvals across tool calls is allowed only within the documented replay window with the same arguments and the same skill ID.
5. **Path traversal**: any code that takes a relative path from a manifest, config, or API input and reads it MUST normalize via `filepath.Clean` and verify with `strings.HasPrefix(absPath, allowedRoot)`. There is a helper in `internal/skills/source/localdir.go` — use it; don't reinvent.
6. **CSP for MCP Apps**: `text/html` content from `ui://` resources is wrapped with the configured CSP at `internal/apps/csp.go`. Don't bypass.
7. **Secret redaction on audit emit**: every payload goes through `audit.Redactor`. Don't write events bypassing it.
8. **No untyped tool arguments in audit payloads.** Summarize, truncate, or redact — full args are not safe to persist.
9. **Process command allowlist**: stdio servers run user-supplied commands. The supervisor enforces an optional allowlist (`auth.command_allowlist` per server). When implementing future runtime modes, preserve this check.
10. **No `exec.Command` with shell strings.** Always argv-form (`exec.Command("npx", "-y", "@foo/bar")`), never `sh -c "..."`.

---

## 8. MCP protocol rules

- The protocol version Portico targets is pinned in `internal/mcp/protocol/types.go` as `ProtocolVersion`. Bumping the version is an RFC change, not a code change.
- All wire types live in `internal/mcp/protocol/`. Other packages import them; nothing else defines MCP message structs.
- Method name constants live in `internal/mcp/protocol/methods.go`. Don't hardcode `"tools/list"` strings elsewhere.
- Error codes live in `internal/mcp/protocol/errors.go`. Add new codes there and only there.
- Northbound transport (HTTP+SSE) is in `internal/mcp/northbound/http/`. Stdio northbound (for embedding) is post-V1; do not add it without an RFC update.
- Southbound clients implement the `southbound.Client` interface. Adding a new transport (e.g. WebSocket) requires extending the interface in one place; don't fork.
- Capability negotiation: never advertise a capability the dispatcher does not implement. The aggregator at `internal/mcp/protocol/capabilities.go` is the single source.
- Server-initiated requests (Phase 5: elicitation): use the request correlator at `internal/mcp/northbound/http/server_initiated.go`. Don't open a second SSE channel.
- Trace context: inject via `internal/telemetry/propagation.go`. HTTP southbound gets `traceparent` header; stdio gets `MCP_TRACEPARENT` env on spawn and `_meta.traceparent` per request.

---

## 9. Storage / SQLite

- **One DB driver:** `modernc.org/sqlite`. CGo-free.
- **Migrations** live in `internal/storage/sqlite/migrations/NNNN_<slug>.sql`. Numbered monotonically. Each migration ends with `INSERT OR IGNORE INTO schema_migrations(version) VALUES (N);`.
- Migrations are **forward-only** in V1. If you need to undo something, write a new migration that does the undo. Don't edit a merged migration.
- All queries are parameterized. No string concatenation into SQL.
- Use `WAL` journal mode (set in `0001_init.sql`). Don't change without an RFC update.
- Postgres is post-V1. Don't add Postgres-specific syntax to migrations. Reserve features that aren't in SQLite for a future migration set.
- Transactions: use `db.BeginTx(ctx, ...)` and defer rollback before commit. Long-running ops outside transactions.

---

## 10. Configuration changes

- Schema lives in `internal/config/config.go`. New fields require:
  1. Backward compatibility (new optional fields with documented defaults), OR
  2. An RFC update with a documented migration path.
- Validation lives in `internal/config/loader.go::Validate`. New fields validated there.
- Hot-reloadable fields documented in the phase plan that introduces them. If unsure, the rule is **not hot-reloadable** by default; restart-required.
- `portico.yaml` examples in `examples/` updated whenever the schema gains a top-level field.

---

## 11. Testing rules

- Tests named per phase plans MUST exist and pass. Don't rename without updating the plan.
- New code paths require new tests. PRs that add code without tests are rejected.
- Race detector is mandatory: `go test -race`. CI matrix runs Linux + macOS.
- Integration tests use `httptest` for HTTP downstream and `examples/servers/mock/cmd/mockmcp` for stdio downstream.
- Coverage gates per phase plan. If you can't hit the gate, the design is too entangled — refactor.
- **Cross-tenant isolation**: any new code touching multiple tenants must have an integration test asserting isolation.
- **Time-sensitive tests** use a controllable clock. Never `time.Sleep` in tests for synchronization (it's flaky); use channels or `eventually` helpers.
- **Goroutine leak tests**: long-lived components have a test that asserts `runtime.NumGoroutine` returns to baseline after shutdown.

---

## 12. Commit and PR conventions

### Commits

- **Conventional Commits** style: `<type>(<scope>): <subject>`.
  - Types: `feat`, `fix`, `docs`, `refactor`, `test`, `perf`, `build`, `ci`, `chore`, `deps`.
  - Scope is the most-affected package or area: `phase-2`, `mcp/southbound`, `vault`, `ci`, etc.
- Subject in imperative mood, no trailing period, ≤ 72 chars.
- Body explains **why**, not what. Reference RFC sections / phase plan sections / issue numbers.
- One commit = one logical change. Squash WIP locally before push.

### Pull requests

- Use the PR template (`.github/PULL_REQUEST_TEMPLATE.md`).
- Tag the phase being implemented when applicable.
- All checklist items must be addressed (✅ done / ❌ N/A with reason / ⏳ explicitly deferred to follow-up issue).
- Self-review before requesting review.
- A PR that needs to update the RFC and the phase plan should do so in the **same** PR or as a small dedicated doc PR landed first.

### Merge

- Squash merge by default. Maintain a clean linear history on `main`.
- Force-push to feature branches is fine; force-push to `main` is forbidden.
- `main` is the release branch. Tagged releases (`vX.Y.Z`) are made from `main` only.

---

## 13. Forbidden practices

These will cause the PR to be rejected on sight.

- ❌ Hardcoded secrets, including in tests. Use `testdata/` fixtures with documented dummy values.
- ❌ Shell-form `exec.Command("sh", "-c", "...")`.
- ❌ HS\* / `none` JWT algorithms.
- ❌ Storing tenant identity in package-level state.
- ❌ Adding a third place to define MCP message types (single source: `internal/mcp/protocol`).
- ❌ Using `panic` for control flow.
- ❌ Adding CGo dependencies. Build is `CGO_ENABLED=0`.
- ❌ Pulling in heavy frameworks (web frameworks, ORM libraries, RPC frameworks). Stdlib + the libraries in RFC §11.2 are the allowed surface; additions require RFC update.
- ❌ Logging unredacted tool arguments or results.
- ❌ Bypassing the approval flow for any reason.
- ❌ Cross-tenant queries without an explicit `admin` scope check.
- ❌ Editing migrations after they have merged. Append-only.
- ❌ Adding `//nolint:` without a one-line reason.
- ❌ `go test` without `-race`.
- ❌ `git push --force` to `main`.
- ❌ Committing with `--no-verify` to skip the preflight hook except in a documented emergency. Doing so without justification is treated the same as merging a broken build.
- ❌ Adding a new HTTP endpoint or MCP method without extending the relevant `scripts/smoke/phase-N.sh` to exercise it.

---

## 14. Pre-merge checklist

Before requesting review, run through this:

- [ ] `make vet test build` passes locally.
- [ ] `golangci-lint run` is clean.
- [ ] **`make preflight` passes locally** (build + boot + HTTP smoke against impacted/new surfaces).
- [ ] If you added an endpoint or MCP method, the relevant `scripts/smoke/phase-N.sh` exercises it and asserts response shape.
- [ ] Coverage on touched packages is ≥ phase target (or this PR explicitly improves it toward the target).
- [ ] If multi-tenant code paths changed: cross-tenant isolation test passes.
- [ ] If MCP types changed: every reference still compiles, including northbound and southbound clients.
- [ ] If config schema changed: example configs in `examples/` updated; backward compatibility verified.
- [ ] If migrations added: clean DB starts cleanly; existing DB runs the new migration; both via tests.
- [ ] AGENTS.md and CLAUDE.md still verbatim identical (`make check-mirror`).
- [ ] No new TODO comments without an issue link.
- [ ] No leftover `fmt.Println` / `log.Print*`.
- [ ] No new dependencies without one-liner rationale in the PR description.
- [ ] PR description references RFC section / phase plan section.
- [ ] CHANGELOG (when it exists post-V1) updated.

If you are an AI agent: **do not claim a task is done until every applicable checklist item is verified.** "I think tests pass" is not verification. Run the commands; show the output. In particular, "preflight passed" requires actually running `make preflight` and reading the OK/SKIP/FAIL counts at the end.

---

## 15. When in doubt

- If a rule contradicts a phase plan, the **plan wins** (this file is operational, the plan is design).
- If a plan contradicts the RFC, the **RFC wins**.
- If something is unspecified, propose the smallest change that solves the problem and link to it in the PR description.
- If you discover a rule that should exist but doesn't, add it to this file and `CLAUDE.md` (verbatim copy) in your PR.

---

## 16. Mirroring

`AGENTS.md` and `CLAUDE.md` are kept verbatim identical. After any edit, run:

```bash
diff -q AGENTS.md CLAUDE.md
# expected: no output (files identical)
```

**CI enforces this invariant.** The `docs` job in `.github/workflows/ci.yml` runs `diff -q AGENTS.md CLAUDE.md` and fails the build if they differ. If they drift in a PR, the contributor must reconcile before merge — typically by copying the file with the intended changes over the other.
