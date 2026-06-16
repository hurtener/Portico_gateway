# Portico autonomous-build engine

The **engine room** for the cadence in [`docs/ORCHESTRATION_PLAYBOOK.md`](../docs/ORCHESTRATION_PLAYBOOK.md):
a hybrid model split where a free/cheap **builder** (NVIDIA NIM Nemotron-3 Ultra 550B)
running unattended in this devcontainer does the mechanical volume, and the **orchestrator**
(a capable model — Claude) plans the work, writes the per-unit brief, adversarially verifies,
fixes the hard parts, and owns the merge gate.

This engine is **plan-driven**, which is the key difference from the WorkBridge cadence the
method was proven on: the loop runs a single generic `portico-build` command, and the unit of
work is set entirely by [`TASK.md`](./TASK.md), which the orchestrator rewrites per unit. The
builder never decides scope — it re-orients from git + `TASK.md` + `CLAUDE.md` + the named
phase plan every fresh, stateless iteration.

> Full method (container lifecycle, stateless-loop rationale, the verify-don't-trust depth,
> and the gotchas checklist) lives in the personal `orchestrate-autonomous-build` Claude
> skill. This README is the operational quickstart for *this* repo.

## Files

| File | Role |
|---|---|
| `Dockerfile` | Go 1.25 + Node 22 + golangci-lint (CI-pinned v1.64.8) + gh + Bun + opencode. |
| `devcontainer.json` | Bind mounts (repo, read-only secrets, runtime `var/`). |
| `entrypoint.sh` | Runtime secret injection (opencode auth, NIM key, gh token) + git identity. |
| `run.sh` | **Host** script: prep secrets → build image → recreate container → launch loop. |
| `loop.sh` | Outer loop (PID 1): fresh `opencode run --command portico-build` per iteration, NIM primary, rate-limit backoff, stop-token detection. |
| `opencode.json` | Repo-level: NIM provider + the plan-driven `portico-build` command template. |
| `opencode-global.json` | Plugins + `/goal` command (baked into the image). |
| `TASK.md` | **The per-unit brief.** Orchestrator-owned; rewritten before each loop. |
| `secrets/`, `var/` | Gitignored. Injected secrets + run logs. Never committed. |

## Secrets (gitignored — never committed)

`run.sh` populates `secrets/` on each launch:
- `gh_token` — from `gh auth token` (personal `hurtener` account).
- `auth.json` — copied from `~/.local/share/opencode/auth.json`.
- `nvidia_api_key` — kept as the existing gitignored file, or overridden via `NVIDIA_API_KEY`.

The model and git identity are pinned to the **personal** account (`hurtener` /
`benvenuto.santiago@hotmail.com`) — never the work identity.

## Run one unit

```bash
# 1. Orchestrator writes the target into TASK.md (one coherent unit).
# 2. Launch the loop (builds the image on first run):
./.devcontainer/run.sh

# 3. Watch it:
tail -f .devcontainer/var/run.log
cat .devcontainer/var/status.txt        # RUNNING | COMPLETE | BLOCKED | RATE_LIMITED | MAX_ITERS_REACHED

# Short slice (cap iterations):
MAX_ITERS=50 ./.devcontainer/run.sh
```

Switch units by editing `TASK.md` and **re-running `run.sh`** (it `docker rm -f`s the old
container and recreates it). Never `docker start` a stopped container — that resurrects the
old loop with stale task env and causes dual-loop API contention.

## Orchestrator monitoring cadence (~every 20 minutes)

The loop is unattended, but it is **not** fire-and-forget. The orchestrator checks in on a
~20-minute timer and reports back. The rule: **don't interrupt a loop that is moving; relaunch
one that is stale.**

Each check:
1. `cat .devcontainer/var/status.txt` — terminal state? (`COMPLETE` / `BLOCKED` → stop loop, start verification.)
2. `tail -n 40 .devcontainer/var/run.log` — is the iteration counter advancing?
3. `git -C . log --oneline -5` and `git status` — are files actually changing, or is it "active but unproductive"?
4. Confirm the log still shows the pinned NIM model (not a silent reroute).
5. **Stall test:** if `run.log` mtime and `git status` are frozen for many minutes and the
   counter isn't advancing, the iteration is wedged (compaction stall / hung process).
   **Relaunch:** `./.devcontainer/run.sh` (recreate). A moving loop is left alone.

Report to the user each cycle: status, iterations done, what changed in git, and "moving" vs
"stale → relaunched". Widen the cadence once a slow model is steadily progressing —
over-monitoring tempts premature kills that cost more than they save.

Keep the host awake for long runs:
```bash
caffeinate -dimsu &           # macOS; stop it when the loop ends
```

## After a unit reports `[goal:complete]`

Completion is the **start** of verification, never the end (playbook §4, skill §7). The
orchestrator independently: runs every machine gate, reads the actual handler/component
bodies for the stub trap, counts artifacts against claims, and live-validates the surface
(Console via Playwright on `127.0.0.1:8080`, the Playground, the smoke checks). Findings get
written down with `file:line` evidence and fed back as the next narrow `TASK.md` unit. Only a
verified unit gets committed, PR'd, and merged by the orchestrator.
