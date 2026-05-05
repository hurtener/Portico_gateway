#!/usr/bin/env bash
#
# Portico preflight: build the binary, boot the dev server, run phase smoke
# tests against the running server, tear it down. CI and pre-commit hook use
# this same script.
#
# Pre-Go-code: gracefully no-ops with a clear notice.
# Per-phase: phase-N smoke scripts auto-skip surfaces that aren't built yet.
#
# Usage:
#   bash scripts/preflight.sh
#
# Env:
#   PORTICO_PREFLIGHT_PORT     port to bind (default 18080)
#   PORTICO_PREFLIGHT_TIMEOUT  seconds to wait for /healthz (default 30)
#   PORTICO_PREFLIGHT_SKIP=1   skip the entire preflight (emergency only;
#                              must be justified in the PR description)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PORT="${PORTICO_PREFLIGHT_PORT:-18080}"
TIMEOUT="${PORTICO_PREFLIGHT_TIMEOUT:-30}"
LOG="$(mktemp -t portico-preflight.XXXXXX.log)"
SERVER_PID=""

# shellcheck source=scripts/smoke/common.sh
. "$REPO_ROOT/scripts/smoke/common.sh"

cleanup() {
  local rc=$?
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null || true
    # give it 3s to drain
    for _ in 1 2 3 4 5 6; do
      kill -0 "$SERVER_PID" 2>/dev/null || break
      sleep 0.5
    done
    kill -KILL "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ $rc -ne 0 ]; then
    say_err "preflight failed (exit $rc)"
    if [ -s "$LOG" ]; then
      echo "--- server log (last 200 lines) ---"
      tail -n 200 "$LOG"
      echo "--- end server log ---"
    fi
  fi
  rm -f "$LOG" 2>/dev/null || true
  return $rc
}
trap cleanup EXIT

if [ "${PORTICO_PREFLIGHT_SKIP:-}" = "1" ]; then
  say_warn "preflight: PORTICO_PREFLIGHT_SKIP=1 — skipping. Justify in the PR."
  exit 0
fi

say_step "Sanity"
require_cmd curl
require_cmd jq

if [ ! -f go.mod ]; then
  say_skip "preflight: go.mod absent — pre-Go-code phase. Skipping build/boot/smoke."
  exit 0
fi

say_step "Build"
make build
make mockmcp || true   # mockmcp is best-effort; phases that need it will skip if absent

if [ ! -x ./bin/portico ]; then
  say_err "./bin/portico not produced by 'make build'"
  exit 1
fi

say_step "Boot dev server on 127.0.0.1:$PORT"

# Use a temp data dir so preflight does not pollute the working tree.
TMPDIR_PREFLIGHT="$(mktemp -d -t portico-preflight.XXXXXX)"
trap 'rm -rf "$TMPDIR_PREFLIGHT"' RETURN
export PORTICO_DEV_TENANT="${PORTICO_DEV_TENANT:-preflight}"

# Most phases boot via dev mode (synthesizes the dev tenant, no JWT).
# Bind to a non-default port to avoid collisions with a developer's running instance.
./bin/portico dev \
  --bind "127.0.0.1:$PORT" \
  --data-dir "$TMPDIR_PREFLIGHT" \
  > "$LOG" 2>&1 &
SERVER_PID=$!

if ! wait_for_health "$PORT" "$TIMEOUT"; then
  say_err "server failed to become healthy within ${TIMEOUT}s"
  exit 1
fi

export PORTICO_PREFLIGHT_PORT="$PORT"
export PORTICO_PREFLIGHT_BASE_URL="http://127.0.0.1:$PORT"

# Run each phase smoke script. Each is independent and reports its own
# OK/SKIP/FAIL counts; this orchestrator aggregates.
say_step "Smoke tests"
TOTAL_OK=0
TOTAL_SKIP=0
TOTAL_FAIL=0
FAILED_PHASES=()

for phase_script in "$REPO_ROOT"/scripts/smoke/phase-*.sh; do
  phase_name="$(basename "$phase_script" .sh)"
  echo
  echo "===> $phase_name"
  set +e
  bash "$phase_script"
  rc=$?
  set -e
  # Each phase script writes its counters to ./.preflight-counters
  if [ -f "$REPO_ROOT/.preflight-counters" ]; then
    # shellcheck disable=SC1091
    . "$REPO_ROOT/.preflight-counters"
    TOTAL_OK=$((TOTAL_OK + ${PHASE_OK:-0}))
    TOTAL_SKIP=$((TOTAL_SKIP + ${PHASE_SKIP:-0}))
    TOTAL_FAIL=$((TOTAL_FAIL + ${PHASE_FAIL:-0}))
    rm -f "$REPO_ROOT/.preflight-counters"
  fi
  if [ $rc -ne 0 ]; then
    FAILED_PHASES+=("$phase_name")
  fi
done

echo
say_step "Summary"
echo "  OK:   $TOTAL_OK"
echo "  SKIP: $TOTAL_SKIP   (features not yet implemented in this build — fine)"
echo "  FAIL: $TOTAL_FAIL"
if [ "${#FAILED_PHASES[@]}" -gt 0 ]; then
  say_err "phase scripts with failures: ${FAILED_PHASES[*]}"
  exit 1
fi
if [ "$TOTAL_FAIL" -gt 0 ]; then
  say_err "$TOTAL_FAIL smoke check(s) failed"
  exit 1
fi
say_ok "preflight passed (ok=$TOTAL_OK skip=$TOTAL_SKIP)"
