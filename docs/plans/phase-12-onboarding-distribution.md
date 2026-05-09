# Phase 12 — Onboarding, Distribution, V1 Ship

> Self-contained implementation plan. Builds on Phase 0–11. The phase that turns a working binary into a shipped product.

## Goal

Get Portico to a state a stranger can pick up, run, and operate in under fifteen minutes — and that stays operable across upgrades. Three deliverable families:

1. **Onboarding** — a first-run wizard, a polished `portico init` flow, fixtures that demonstrate the system without external dependencies, and an in-Console help system that walks a new operator through registering a server, authoring a skill, running it from the playground, and inspecting the resulting session.
2. **Documentation site** — a docs website matching the design system, hosted from the same `web/console/static/` machinery (or a sibling sub-route) so the binary ships with self-contained docs. Concept docs, how-tos, REST API reference, MCP method reference, glossary, troubleshooting, conformance summary.
3. **Distribution** — `make release` produces signed multi-arch binaries (linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64), distroless container images (linux-amd64, linux-arm64), an `install.sh` quick-bootstrap, a Homebrew tap formula, an MCP conformance suite that any V1 user can run against their deployment, and a documented upgrade path.

After Phase 12, Portico ships V1. There are no more invariants to add; subsequent phases are additive (LLM gateway in Phase 13, then post-V1 nice-to-haves).

## Why this phase exists

Phases 0–11 produced a feature-rich, multi-tenant, observable, operable system — but none of those phases optimised for the "first thirty minutes." Today an operator who clones the repo and types `make build` lands at a binary, a YAML, and a `--help` flag. They can absolutely make it work; they just have to read the RFC + plans first. That's not what V1 means.

V1 means: download a binary, run a wizard, end up with a functional Portico with a sample server, a sample skill, and a working playground. From there, every other surface is discoverable from the Console. That's the bar this phase clears.

A second motivator: distribution is its own engineering domain. Cross-compilation, signing, image scanning, repository hosting, an upgrade story — none of these are work the engine phases handled, and all of them block "we shipped V1." Phase 12 owns them.

## Prerequisites

Phases 0–11 complete. Specifically:

- Console covers servers + tenants + secrets + policy + skills (authored + sources) + playground + telemetry replay.
- Skill sources support local + git + http + authored (Phase 8).
- Audit + bundle export (Phase 11).
- Existing CI runs `make vet test build`, `npm run check && npm run lint && npm run build`, smoke against every phase.
- `make build` produces `bin/portico` already; CGo-free, static.
- Existing `Dockerfile` produces a distroless image.

Phase 11 explicitly deferred five items to Phase 12 (see "Phase 11 carry-overs" below). They are not optional — V1 cannot ship without them. Treat them as first-class deliverables alongside the V1-ship work.

## Deliverables

1. **First-run wizard** at `/onboarding` (auto-redirect on first start when no tenants exist). Steps: welcome → create admin tenant + JWT issuer → register first server (mock by default; option to skip) → install one example skill (Phase 8 authored or Phase 8 git-source from a hosted reference repo) → run a sample call from the playground → "you're ready" with links into the Console.
2. **`portico init`** — interactive CLI that scaffolds a new operator's `portico.yaml` (listener, JWT, storage paths, log level), seeds an admin JWT signing key and dev tenant, and starts the server in one command. The Console wizard runs against this seed.
3. **Fixture pack** — `examples/onboarding/` carries a self-contained set of fixtures the wizard uses: a mock MCP server (existing `examples/servers/mock`), a small authored skill (manifest + SKILL.md + one prompt), a starter policy ruleset, a saved playground case ("happy path"). Wired into `make seed` so any developer can recreate the demo state.
4. **In-Console help system** — every page gains a "Help" button in the PageHeader that opens a Drawer with: brief explanation, link to the matching docs page, "Show me how" CTA that triggers a guided tour (lightweight, no third-party tour library — a `Coachmark.svelte` primitive).
5. **Docs site** — `docs/site/` is a SvelteKit static-site project (separate `package.json` from the Console, sharing tokens.css). Builds to `web/docs/build/`, embedded into the binary at `/docs/`. Pages: Getting started, Concepts (tenants, servers, skills, sessions, snapshots, drift, approvals, policies, vault), How-tos (register a server, author a skill, set up an OAuth credential, mint a JWT, debug a session), Reference (REST API, MCP methods, configuration, environment variables, SQL schema), Troubleshooting, Conformance summary, FAQ, Glossary.
6. **REST API reference** — generated from typed handler annotations using a small in-repo OpenAPI extractor. Output: `docs/site/src/routes/reference/rest/+page.svelte` driven by a JSON file the build pipeline regenerates. The OpenAPI document is also served at `/api/openapi.json` for tool discovery.
7. **`make release`** — produces signed multi-arch binaries (linux-amd64/arm64, darwin-amd64/arm64, windows-amd64), distroless multi-arch container images, a SHA256SUMS file, optional GPG signature (release key in a separate keystore), and a checksum manifest. Uses `goreleaser`-equivalent in stdlib + a small wrapper so we don't take a heavy dependency.
8. **Container images** — distroless `nonroot` base, multi-arch, scanned by Trivy in CI. Image labels carry version, build date, git SHA, repo URL, license. SBOM (CycloneDX) attached to every image.
9. **Installation entrypoints** — `install.sh` (curl-pipe-able, signature-verified), Homebrew tap (in `hurtener/homebrew-portico` repo, formula generated by the release pipeline), Docker Compose example for a complete stack (Portico + an OTLP collector + a sample MCP server).
10. **MCP conformance suite** — `cmd/portico conformance` runs a battery of tests against any deployed Portico instance to validate it conforms to the MCP 2025-11-25 protocol (initialize, tools/list, tools/call, resources/*, prompts/*, notifications/*, elicitation/*, capability negotiation). Outputs a structured report. Used in CI against the dev binary and offered to operators for their own deployments.
11. **Upgrade path** — documented + tested. `portico upgrade-check` reads the current binary version, queries the release feed, reports available upgrades. Migrations forward-only (Phase 0 invariant) — `portico migrate` runs pending DB migrations on startup; `--dry-run` reports what would change.
12. **Public website primer** (lightweight) — `web/site/` carries a single-page brand site (matching `docs/Design/Portico.png`) with the tagline + "Get started" CTA → docs site. Hosted from the same binary at `/`. Optional: deployable as a separate static site to `portico.dev` (or wherever the operator publishes it).
13. **Bundle encryption (Phase 11 carry-over).** `internal/sessionbundle/crypto.go` lands age-encrypted bundles. `ExportOptions.Encrypt + RecipientKey` flow through to a real encrypted tar.gz; `manifest.encrypted` flips to true. The importer accepts age identities and reverses the operation. CLI flags: `portico session export --encrypt --recipient age1...` / `portico session import --identity ~/.config/portico/age.txt`. Recipient fingerprint is logged on export so operators can correlate "who can read this bundle." Unit + integration tests cover encrypt → import round-trip and refuse-on-bad-identity.
14. **`inspect-session` CLI rewrite (Phase 11 carry-over).** `cmd/portico/cmd_inspect_session.go` keeps its current `--dsn` offline path and adds two more: `--base-url + --token` for live mode (consumes `/api/sessions/{sid}/bundle` with a JWT) and `--bundle <path>` for archive mode (no live URL needed). Shared rendering core so all three modes produce byte-identical JSON for a given input. Parity test enforces this in CI.
15. **Vault root-key rotation (Phase 9 → 11 → 12 carry-over).** `internal/secrets/rotate_root.go` lands the transactional rotation Phase 9 stubbed and Phase 11 deferred. New `vault_keys_archive` table holds the previous master key for a configurable grace window (default 14 days). `POST /api/admin/secrets/rotate-root` replaces the 501 stub; partial-failure semantics — if 999 of 1000 entries succeed and the 1000th fails, the rotation aborts, the active key is restored, and a `vault.rotate_root.aborted` audit event is emitted. `PORTICO_VAULT_KEY_NEXT` env var is the new-key seam. Coverage gate: ≥ 80% on the rotation surface.
16. **`entity_activity` retention sweep (Phase 9 → 11 → 12 carry-over).** Per-tenant retention worker aligned with the existing audit-event retention scheduler. Default 30 days; per-tenant override via `tenants.audit_retention_days` (column already exists from Phase 9 migration 0009). One sweep tick purges audit events, entity_activity rows, AND spans (the existing Phase 11 hourly span sweeper is moved into this unified worker so retention has one observability surface).
17. **Phase 11 perf gates (carry-over).** Three measurements that Phase 11 acceptance criteria (#2, #3, #8) called for but couldn't run without populated data. The Phase 12 fixture pack (`make seed`) generates a corpus large enough to validate these in CI: bundle latency ≤ 200 ms for a typical session (≤ 100 audit events, ≤ 50 spans), inspector first-paint ≤ 1.5 s for 200 events, audit FTS search ≤ 500 ms for a tenant with 100k events. Failures block release.

## Acceptance criteria

1. A fresh clone → `make build` → `./bin/portico init` → wizard completes the demo loop in ≤ 15 minutes on a stock laptop, with no external services and no manual editing of YAML.
2. Every page in the Console has a Help drawer entry. Clicking through every page shows real content, no placeholders.
3. The docs site renders inside the binary at `/docs/` and offline (no CDN). Internal links work; the search box returns results.
4. `make release` produces every artifact in the deliverable list. CI validates checksums and image manifests.
5. Container images pass Trivy scan with zero CRITICAL or HIGH vulns. SBOM attached.
6. `portico conformance` passes against a live `portico dev` instance. Conformance report includes counts: "26/26 MCP method tests passed, 4/4 capability negotiation, 6/6 SSE framing, 2/2 elicitation."
7. `install.sh` works on Linux + macOS without sudo (installs to `~/.local/bin`); signature verification passes.
8. Homebrew tap installs the latest version on macOS Sonoma or later. `brew upgrade portico` works.
9. Cross-tenant isolation invariants from Phase 0 are validated by an integration test that runs *after* the upgrade flow against a populated database.
10. Smoke: `scripts/smoke/phase-12.sh` covers init flow, wizard endpoints, docs page rendering, conformance, openapi.json. SKIP for unimplemented; OK ≥ 12 by phase close.
11. Coverage: ≥ 75% on new packages (`internal/onboarding`, `internal/conformance`, `internal/release`).
12. Documentation completeness: every public REST endpoint has a docs page with example request + response; every MCP method has a reference entry; every config field has a description.
13. **Bundle encryption round-trip (Phase 11 carry-over).** `TestE2E_BundleEncryption_RoundTrip`: export with `--encrypt --recipient age1...`, import with the matching identity, decoded bundle bytes match the source. A bundle encrypted to one recipient cannot be opened with a different identity (typed `bundle_decrypt_failed` error).
14. **`inspect-session` parity (Phase 11 carry-over).** Same input session, three transports (`--dsn`, `--base-url`, `--bundle`), byte-identical JSON output. CI runs the parity check and fails on diff.
15. **Vault rotate-root happy path + abort (Phase 9 → 12 carry-over).** Integration tests cover: (a) full rotation succeeds, archived key purged after grace window; (b) one entry fails mid-rotation → entire transaction rolls back, active key unchanged, `vault.rotate_root.aborted` audit event written; (c) decrypt with archived key during grace window succeeds, after expiry fails.
16. **Retention sweep (Phase 9 → 12 carry-over).** Integration test seeds audit + entity_activity + spans older than the per-tenant cutoff; one sweep tick purges all three; per-tenant override is honoured (tenant A 30 days vs tenant B 7 days produces independent purge results).
17. **Phase 11 perf gates measured (carry-over).** Three CI benchmarks that fail the build on regression: bundle p95 ≤ 200 ms, audit FTS p95 ≤ 500 ms on a 100k-event tenant fixture, inspector first-paint ≤ 1.5 s in Playwright.

## Architecture

```
internal/onboarding/
├── wizard.go                  # state machine (steps, transitions, idempotent re-entry)
├── seeds.go                   # default tenant + JWT signing keys + sample skill
├── fixtures.go                # exported fixture loader for `make seed`
└── wizard_test.go

internal/conformance/
├── runner.go                  # spec test runner
├── tests/                     # per-method test vectors
│   ├── initialize.go
│   ├── tools.go
│   ├── resources.go
│   ├── prompts.go
│   ├── elicitation.go
│   └── capabilities.go
├── report.go                  # structured + text report
└── runner_test.go

internal/release/
├── builder.go                 # cross-compile + checksum + image manifest
├── signer.go                  # gpg sign + sigstore attest (optional)
├── notes.go                   # release notes from CHANGELOG
└── builder_test.go

internal/server/
├── api/openapi.go             # extracts OpenAPI from typed handler annotations
├── api/onboarding.go          # /api/onboarding/* — wizard back-end
├── docs/embed.go              # mounts docs site under /docs/
└── site/embed.go              # mounts public website under /

cmd/portico/
├── cmd_init.go
├── cmd_conformance.go
├── cmd_upgrade_check.go
└── cmd_migrate.go             # exposed as a subcommand even though startup runs it

web/console/src/lib/components/
├── Coachmark.svelte           # tour overlay primitive
├── HelpDrawer.svelte
└── …

web/console/src/routes/onboarding/+page.svelte
web/docs/                      # SvelteKit static-site project
web/site/                      # SvelteKit static-site project (single page)

examples/onboarding/
├── tenant.yaml
├── skill/                     # authored skill bundle
├── policy.yaml
└── playground-cases.json

scripts/release/
├── build.sh                   # entrypoint → invokes `portico release`
├── images.sh                  # buildx + push
└── attest.sh                  # cosign / gpg

Makefile
├── seed                       # populate fixture data into a fresh DB
├── release                    # invoke release builder
├── docs                       # build the docs site
└── site                       # build the public website
```

## SQL DDL (no new tables)

Onboarding state is ephemeral — first run is detected from existing tables (tenant count == 0 → first-run mode). No migration required.

## Public types

```go
// internal/onboarding/wizard.go

type Step string

const (
    StepWelcome     Step = "welcome"
    StepTenant      Step = "tenant"
    StepFirstServer Step = "server"
    StepFirstSkill  Step = "skill"
    StepPlayground  Step = "playground"
    StepDone        Step = "done"
)

type State struct {
    Current      Step
    CompletedAt  map[Step]time.Time
    SeedTenantID string
    SeedServerID string
    SeedSkillID  string
}

type Wizard interface {
    State(ctx context.Context) (State, error)
    Advance(ctx context.Context, payload map[string]any) (State, error)
    Reset(ctx context.Context) error
}
```

```go
// internal/conformance/runner.go

type Test struct {
    Name        string
    Description string
    Run         func(ctx context.Context, c *Client) Result
}

type Result struct {
    Pass    bool
    Skipped bool
    Reason  string
    Spans   []TestSpan          // request/response pairs
}

type Report struct {
    Total   int
    Passed  int
    Failed  int
    Skipped int
    Suites  []SuiteReport
}

type SuiteReport struct {
    Name    string
    Results []TestResult
}

type TestResult struct {
    Test   Test
    Result Result
}

func RunAll(ctx context.Context, c *Client) Report
```

```go
// internal/release/builder.go

type Build struct {
    OS       string  // linux, darwin, windows
    Arch     string  // amd64, arm64
    Path     string
    Checksum string
}

type ReleaseManifest struct {
    Version string
    BuiltAt time.Time
    GitSHA  string
    Builds  []Build
    Image   ImageManifest
    SBOM    SBOMRef
    Signed  bool
}

type ImageManifest struct {
    Repository string
    Tags       []string
    Digests    map[string]string  // arch → sha256:…
}

func BuildAll(ctx context.Context, opt Options) (ReleaseManifest, error)
```

## REST API

```
GET    /api/onboarding/state                  → current wizard state
POST   /api/onboarding/advance                → step the wizard; idempotent on the same payload
POST   /api/onboarding/reset                  → clear seed data; admin only

GET    /api/openapi.json                      → OpenAPI 3.1 of the live REST surface
GET    /api/health/conformance                → "summary" (last conformance run if any)

GET    /docs/*                                → docs site (served from embed.FS)
GET    /                                      → public site (served from embed.FS)
```

The Console's auto-redirect to `/onboarding` on first run is a single check on the Console layout: `if (state.completed_at.welcome === undefined) goto /onboarding`.

## CLI

```bash
# new
portico init                                  # interactive scaffolder; writes portico.yaml + key
portico init --non-interactive --output ./dir # CI-friendly mode

portico conformance --target http://localhost:8080 --token "$JWT"
portico conformance --target http://localhost:8080 --token "$JWT" --output json

portico upgrade-check                         # reports current vs. latest
portico migrate                               # runs pending migrations; --dry-run reports
portico release --output ./dist               # build all artifacts
portico release notes --since v0.5.0          # generate release notes

# existing, polished
portico vault put|get|delete|list|rotate-key
portico inspect-session
portico session export|import
portico validate
portico validate-skills
```

## Console screens

### `/onboarding`

Single-page wizard with a horizontal stepper (`Tabs`-based for the visual). Each step is a panel:

1. **Welcome** — logo + tagline + brief paragraph. "Begin" button.
2. **Admin tenant** — Form for tenant id, name, JWT issuer, JWKS URL (defaulted to "use Portico's built-in dev keys"). Submitting creates the tenant + signs an operator JWT and stores it in localStorage.
3. **First server** — choice: register the bundled mock MCP server (default) or skip to bring your own. Shows a live preview of the tools the mock exposes.
4. **First skill** — choice: install the example authored skill (a one-prompt "summarise an audit log" skill) or skip. Shows the manifest + SKILL.md preview.
5. **Try the playground** — opens an embedded `/playground` slice scoped to the new tenant. Pre-fills a sample call. Operator clicks "Run" and watches the streamed response.
6. **Done** — summary: "You created tenant X, registered server Y, installed skill Z, and ran one tool call. Here's where to go next." Links into Servers, Skills, Policy, Telemetry replay.

The wizard state survives reload. Coming back later auto-resumes at the last completed step.

### Help system

Every PageHeader gains a `Help` button. Clicking opens a `Drawer` (Phase 7 primitive) with:

- A brief explanation of what the page does.
- Two or three task-oriented links ("Register a new server", "Edit env vars", "Restart safely").
- A `Show me how` CTA that triggers an in-page tour: a series of `Coachmark.svelte` overlays anchored to specific UI elements, with prev/next/skip buttons.

Tours are static JSON in `web/console/src/lib/help/tours.json` per page. Coachmarks are dismissible and remembered (`localStorage.setItem('tour:servers:done', '1')`).

### Docs site

Top-level routes:

- `/docs/` — hub with the shape of the existing brand site (logo + tagline + nav).
- `/docs/getting-started`
- `/docs/concepts/{tenants,servers,skills,sessions,snapshots,drift,approvals,policies,vault}`
- `/docs/how-to/{register-a-server,author-a-skill,oauth-credentials,mint-a-jwt,debug-a-session,...}`
- `/docs/reference/{rest,mcp,configuration,environment,sql-schema}`
- `/docs/troubleshooting`
- `/docs/conformance`
- `/docs/glossary`
- `/docs/faq`

Pages render in a centred 720 px column with a sidebar of section headings. Code blocks use the same Phase 7 `CodeBlock`. Cross-page search uses `pagefind` (build-time index, no runtime CDN).

### Public site

Single-page hero with logo, tagline ("A governed gateway for MCP servers"), three feature tiles, "Get started" CTA → `/docs/getting-started`, footer. Matches the brand site mock from `docs/Design/Portico.png`.

## Implementation walkthrough

### Step 1 — Onboarding back-end

`internal/onboarding/wizard.go` implements the State Machine with idempotent transitions. Seed data lives in `examples/onboarding/`; loader reads at runtime, no embedded fixtures inside the binary unless the operator opts in (`--with-fixtures` on `init`).

### Step 2 — `portico init`

Interactive prompt (using a tiny stdlib + `bufio` shell — no heavy CLI library). Generates: `portico.yaml`, an Ed25519 signing keypair for dev JWTs (`./.portico/keys/`), an empty `./.portico/data/` for SQLite. Prints a one-line "next: ./bin/portico serve --config portico.yaml" pointer.

`--non-interactive` mode reads defaults from flags + env; produces a known-good baseline for CI runs.

### Step 3 — Wizard frontend + Help drawers

Wire `/onboarding` to the back-end state. Help drawer is a generic `HelpDrawer.svelte` that takes a `topic` prop; topic content lives in markdown files at `web/console/src/lib/help/<topic>.md` rendered at build time.

`Coachmark.svelte` is anchored via `@anchor` data attributes on target elements (no DOM querying tricks); a small `tour.ts` store owns step transitions.

### Step 4 — Docs site

`web/docs/` is a SvelteKit project with `@sveltejs/adapter-static`. Reuses `tokens.css` from the Console (symlink at build time, or copied via `npm run prepare`). Pages are MDsveX (`.svx`) so the prose is markdown but components are available where useful.

The build pipeline:
1. Generate `docs/site/src/lib/openapi.json` from the live OpenAPI extractor.
2. Render every `.svx` page.
3. Build pagefind index over the rendered HTML.
4. Output to `web/docs/build/`.

The Go binary `embed`s `web/docs/build/**` and serves it under `/docs/`.

### Step 5 — OpenAPI extractor

Each REST handler is annotated with a `// @api` block that the extractor parses. Output lands in `internal/server/api/openapi_gen.go` (committed) and at `/api/openapi.json` at runtime. CI runs the extractor and fails if generated output drifts.

Annotation format:

```go
// @api POST /api/skill-sources
// @summary Add a skill source
// @scope skills:write
// @body application/json source.AddRequest
// @response 201 source.Source
// @response 400 errors.Validation
func handleAddSkillSource(w http.ResponseWriter, r *http.Request) { /* … */ }
```

### Step 6 — Conformance suite

`internal/conformance/runner.go` opens an MCP session against the target, runs the suite, produces a `Report`. Tests cover:

- `initialize` with valid + invalid params.
- Capability negotiation (server advertises only what it supports).
- `tools/list` shape, namespacing.
- `tools/call` round-trip, streamed response, error frames.
- `resources/list`, `resources/read`, template substitution.
- `prompts/list`, `prompts/get`, message rendering.
- `notifications/list_changed` after a registry CRUD.
- `elicitation/create` round-trip; cancel; timeout.
- W3C TraceContext propagation (server echoes back the traceparent it saw).
- Error code taxonomy matches `internal/mcp/protocol/errors.go`.

`portico conformance` runs the suite + writes a structured report. CI runs against `portico dev` on every push.

### Step 7 — Release builder

`internal/release/builder.go`:

1. `go build` for each (os, arch) tuple. CGo disabled. `-ldflags='-s -w -X main.Version=$VERSION -X main.GitSHA=$SHA'`.
2. Compute SHA256 per binary, write to `SHA256SUMS`.
3. Build container images via `buildx` (multi-platform); push to `ghcr.io/hurtener/portico` (or operator's registry).
4. Generate SBOM (CycloneDX) per binary + image.
5. Optional sign: GPG over `SHA256SUMS`; sigstore cosign attest the images.
6. Write `release-manifest.json` with everything above.
7. Generate Homebrew formula from the manifest + push to the tap repo.

CI runs `make release --dry-run` on every PR; an actual release is gated on a tagged push.

### Step 8 — Migration runner

`portico migrate` exposes the existing migration system as a CLI subcommand. `--dry-run` prints what migrations would run; default behaviour applies them. Server startup also runs pending migrations (idempotent), so operators rarely run this manually — but it's there for ops who want to run migrations during a maintenance window.

### Step 9 — `install.sh`

Curl-pipe-able. Detects platform, fetches the right binary from the release feed, verifies SHA256, optionally GPG signature (if `gpg` available), extracts to `~/.local/bin`. Prints next-step pointer.

```bash
curl -sSL https://get.portico.dev | sh        # interactive
curl -sSL https://get.portico.dev | sh -s -- --version v1.0.0 --prefix ~/.local
```

### Step 10 — Public website + brand polish

`web/site/+page.svelte` reuses Console primitives + tokens. Single page; extensible later with blog, changelog, integrations gallery (post-V1).

### Step 11 — Smoke + tests

`scripts/smoke/phase-12.sh` covers wizard state machine, openapi.json shape, docs page accessibility, conformance summary, version reporting.

## Test plan

### Unit

- `internal/onboarding/wizard_test.go`
  - `TestWizard_AdvanceFromWelcomeToTenant`.
  - `TestWizard_Idempotent_SamePayload`.
  - `TestWizard_Reset`.
- `internal/conformance/tests/*_test.go` — per suite.
- `internal/conformance/runner_test.go` — happy path + skip-on-incompatible-server.
- `internal/release/builder_test.go`
  - `TestBuilder_AllPlatforms`.
  - `TestBuilder_Checksum_Stable`.
  - `TestBuilder_DryRun_NoArtifacts`.
- `internal/server/api/openapi_test.go`
  - `TestOpenAPI_GeneratedMatchesAnnotations`.
  - `TestOpenAPI_AllRoutesDocumented`.

### Integration (`test/integration/v1_ship/`)

- `TestE2E_FreshInstall_WizardCompletes` — tear down DB, run `portico init --non-interactive`, drive wizard via REST, end with a tool call run from the playground.
- `TestE2E_DocsSite_LinksWork` — load every docs page in headless Chromium; assert no 404 / broken links.
- `TestE2E_Conformance_AgainstDev` — run `portico conformance --target $URL`; assert all tests pass.
- `TestE2E_Release_DryRun` — `make release --dry-run` produces a manifest with the expected build set.
- `TestE2E_Migrate_DryRun_NoChange` — running migrate twice in a row produces zero changes the second time.
- `TestE2E_TenantIsolation_PostUpgrade` — populate v1.x DB, upgrade to vCurrent, isolation invariants hold.

### Frontend tests

- `web/console/tests/onboarding.spec.ts` — wizard happy path; resume after reload.
- `web/console/tests/help.spec.ts` — every page's Help drawer renders content + tour starts.
- `web/docs/tests/docs.spec.ts` — pagefind search returns relevant pages; nav sidebar collapses correctly.

### Smoke

`scripts/smoke/phase-12.sh`:
- GET `/api/onboarding/state` (post-init, expect step=`welcome` or `done`).
- POST `/api/onboarding/advance` with each step payload.
- GET `/api/openapi.json` and assert it parses as valid OpenAPI 3.1.
- GET `/docs/getting-started` (200).
- GET `/` (public site, 200).
- `./bin/portico conformance --target http://localhost:18080 --token "$JWT" --output json` exit 0.
- `./bin/portico version` matches the `-X main.Version` ldflag.

OK ≥ 12, FAIL = 0.

### Coverage gates

- `internal/onboarding`: ≥ 80%.
- `internal/conformance`: ≥ 80%.
- `internal/release`: ≥ 75%.
- `internal/server/api/openapi.go`: ≥ 80%.

## Common pitfalls

- **First-run JWT keypair persistence.** The default Ed25519 key generated by `portico init` lives at `./.portico/keys/`. If the operator deletes it, every issued token breaks. The wizard's done-step prints a clear "Save these keys" pointer + an export command.
- **Docs site asset paths.** Embedded under `/docs/`, pages must use root-relative URLs that resolve under that prefix. Double-check pagefind asset paths after the embed.
- **OpenAPI drift.** The generator is committed (`openapi_gen.go`) so a new endpoint that forgets the annotation breaks CI before merge. Don't edit the generated file by hand.
- **Conformance suite false positives.** Tests must fail clearly, with a reason that points at the spec section. A red conformance report should tell an operator exactly what to fix.
- **Release signing key management.** The release pipeline assumes the GPG / cosign key is in a keystore CI can access. Document the key rotation path explicitly; key-loss is a release-blocker.
- **Multi-arch image manifests.** It's easy to push only the linux-amd64 image and forget arm64. The release builder validates the manifest list before tagging.
- **Container vulnerability churn.** A passing Trivy today fails next week when the database refreshes. CI runs Trivy on the *binary's* deps and the *image's* base layers; flag-but-don't-fail on Medium and below; fail-on-fail Critical+High.
- **Upgrade migration crashes.** A bad migration on a populated DB can wedge a deployment. The migrator runs in a transaction where SQLite supports it (DDL is mostly transactional). `--dry-run` is the safety net; documentation tells operators to back up first.
- **Help drawer content rot.** Drawer markdown files live next to each page's source so they're noticed during PR review. CI grep ensures every `+page.svelte` has a matching `<page>.md` help file.
- **Public site SEO.** The public site is served from `/`; if the operator deploys behind a reverse proxy on a custom domain, OG tags + sitemap + robots.txt must work. Provide template tags via env (`PORTICO_PUBLIC_URL`).
- **Conformance against the operator's own deployment.** Some operators run behind an internal-only auth proxy; the suite must accept a custom `--auth-header` flag to pass through that proxy.

## Out of scope

- **Mobile-friendly Console.** The Console targets desktop ops; mobile is post-V1.
- **In-Console docs editing.** Operators read docs; editing them is a docs-site PR. Authoring docs from the Console is post-V1.
- **Translation / i18n of docs.** English only at V1.
- **Auto-update.** The binary doesn't self-update; operators upgrade manually via the install script. Auto-update is a Phase 15+ topic.
- **Hosted SaaS.** Out of scope per RFC §15.
- **Marketplace for skills/servers.** Skill sources cover discovery; a curated marketplace is post-V1.

## Phase 11 carry-overs (binding)

Phase 11 closed as MVP-complete with five items explicitly deferred. Phase 12 owns landing them — they are enumerated above as Deliverables 13–17 and Acceptance criteria 13–17. Reproduced here as a single-page index so they don't get lost in the body of the V1-ship work:

| # | Item | Driver | Plan reference | Coverage gate |
|---|------|--------|---------------|---------------|
| 1 | Bundle encryption (age) | Operator wants to share bundles cross-org / regulated | Plan §"Deliverables" item 6 (Phase 11) | ≥ 80% on `internal/sessionbundle/crypto.go` |
| 2 | `inspect-session` CLI rewrite (`--base-url`, `--bundle`) | Offline triage parity with the live binary | Plan §"CLI" + §"Step 7" (Phase 11) | parity test in CI |
| 3 | Vault root-key rotation | Phase 9 deferred to Phase 11; Phase 11 deferred to here | Plan §"Phase 9 carry-overs" (Phase 11) | ≥ 80% on rotate-root surface |
| 4 | `entity_activity` retention sweep | Phase 9 deferred to Phase 11; Phase 11 deferred to here | Plan §"Phase 9 carry-overs" (Phase 11) | integration test |
| 5 | Phase 11 perf gates | Bundle / inspector / audit-FTS latency gates | Plan §"Acceptance criteria" 2/3/8 (Phase 11) | CI bench fails on regression |

Each carry-over has a matching deliverable + acceptance criterion above. Do not close Phase 12 without them — the V1 promise (multi-tenant, observable, rotatable, scrubbable) breaks if any of these are missing on day one.

## Done definition

1. All acceptance criteria pass (including the five Phase 11 carry-overs, AC 13–17).
2. `make preflight` green; `scripts/smoke/phase-12.sh` shows OK ≥ 12, FAIL = 0; prior smokes unaffected.
3. Coverage gates met (including the Phase 11 carry-over coverage targets).
4. `portico conformance` against `portico dev` passes 100%.
5. `make release --dry-run` produces every artifact in CI.
6. The docs site renders every page; pagefind index built; no broken internal links.
7. README at repo root rewritten as the V1 quickstart (single screenful; deep links into the docs site).
8. RFC-001-Portico.md updated to mark V1 as shipped (one-line note in §15).
9. CHANGELOG.md created at repo root with the V1 release entry. Phase 11 carry-overs explicitly named in the V1 entry so future readers know when each landed.
10. The user opens the binary, follows the wizard, finishes the demo loop, and acknowledges V1.

## Hand-off to Phase 13

Phase 13 (LLM gateway via `kreuzberg-dev/liter-llm`) is post-V1 territory but inherits:

- A complete, polished, documented operator surface to extend rather than rebuild.
- The conformance suite shape — Phase 13 grows it with OpenAI-compatible API tests.
- The release pipeline — adding a new subsystem doesn't change how artifacts ship.
- The docs site IA — Phase 13 lands a "LLM gateway" concept doc + how-tos.

Phase 13's first task: integrate `kreuzberg-dev/liter-llm` as a Go dependency, expose an OpenAI-compatible northbound API, register LLM providers as a new entity type alongside servers + skills, bridge tool-use back into the existing MCP gateway, and ship Console screens for model registry + per-tenant key management. The shape mirrors Phase 9 (CRUD + audit + hot-reload) — a deliberate choice so V1 operators can pick it up without learning new patterns.
