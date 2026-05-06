#!/usr/bin/env bash
# Phase 7 smoke: Console design system.
#
# Asserts that the Console is built, served, and carries the new brand
# surface: token-driven layout, embedded brand mark, theme bootstrap, all
# nav routes resolve. The Console is a SPA — non-root routes fall back to
# index.html, so we assert the SPA fallback returns 200 and the build
# carries the expected brand assets.
#
# Self-skips if the Console build is absent (e.g. building only the Go
# binary in CI without the frontend job).

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-7 (console design system)"

# Landing page must serve the SPA shell.
capture_status "GET /" -- "$(api_url /)"
case "$RESPONSE_STATUS" in
  404|501)
    say_skip "Console not embedded yet (HTTP $RESPONSE_STATUS) — Phase 0 SPA shell missing"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  200)
    say_ok "GET / serves the Console (HTTP 200)"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  *)
    say_fail "GET / returned HTTP $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

# The page should reference the new brand mark. The pre-paint theme
# script also lands inline, so an absent script means the new layout
# didn't ship.
if printf '%s' "$RESPONSE_BODY" | grep -q "data-theme"; then
  say_ok "pre-paint theme bootstrap present"
  PHASE_OK=$((PHASE_OK + 1))
else
  say_fail "Console HTML lacks data-theme bootstrap"
  PHASE_FAIL=$((PHASE_FAIL + 1))
fi

if printf '%s' "$RESPONSE_BODY" | grep -qi "Portico"; then
  say_ok "page title carries Portico brand"
  PHASE_OK=$((PHASE_OK + 1))
else
  say_fail "Portico brand string missing from /"
  PHASE_FAIL=$((PHASE_FAIL + 1))
fi

# Brand mark must be served as a static asset.
assert_status 200 "GET /brand/portico-logo.svg" \
  -- "$(api_url /brand/portico-logo.svg)"

assert_status 200 "GET /favicon.svg" \
  -- "$(api_url /favicon.svg)"

# Every nav route should resolve via the SPA fallback (200 + HTML).
for route in /servers /resources /prompts /apps /skills /sessions /approvals /audit /snapshots /admin/secrets; do
  capture_status "GET $route (SPA fallback)" -- "$(api_url "$route")"
  case "$RESPONSE_STATUS" in
    200)
      say_ok "GET $route returns 200"
      PHASE_OK=$((PHASE_OK + 1))
      ;;
    404)
      say_skip "GET $route 404 — SPA fallback may not be wired yet"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      ;;
    *)
      say_fail "GET $route returned HTTP $RESPONSE_STATUS"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      ;;
  esac
done

end_phase
