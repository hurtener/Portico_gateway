#!/usr/bin/env bash
# Phase 10.6 smoke: Console substrate + style.
#
# Verifies that the API surfaces backing the Servers and Skills page
# redesign carry the new substrate fields the typed client expects.
# Also asserts the brand SVG uses currentColor so the Sidebar can
# recolor it on the dark slab.
#
# All checks degrade to SKIP when the underlying surface 404s — the
# script must work against builds that haven't shipped the substrate
# yet without lighting up red.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-10.6 (console substrate + style)"

# 1) Brand SVG uses currentColor so Logo.svelte can recolor it.
capture_status "GET /brand/portico-logo.svg" \
  -- "$(api_url /brand/portico-logo.svg)"
case "$RESPONSE_STATUS" in
  200)
    if printf '%s' "$RESPONSE_BODY" | grep -q 'currentColor'; then
      say_ok "brand SVG uses currentColor"
      PHASE_OK=$((PHASE_OK + 1))
    else
      say_fail "brand SVG missing currentColor — Logo.svelte recolor will not work"
      PHASE_FAIL=$((PHASE_FAIL + 1))
    fi
    ;;
  404|405|501)
    say_skip "/brand/portico-logo.svg not served (HTTP $RESPONSE_STATUS)"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    ;;
  *)
    say_fail "brand SVG returned $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 2) /v1/servers carries the substrate envelope. Register a server first
#    so the response is non-empty; cleanup at the end.
SUB_ID="phase-10-6-smoke-$(date +%s)"
capture_status "POST /api/servers (smoke fixture)" \
  -- -X POST "$(api_url /api/servers)" \
  -H 'Content-Type: application/json' \
  -d "{\"id\":\"$SUB_ID\",\"display_name\":\"smoke fixture\",\"transport\":\"stdio\",\"runtime_mode\":\"shared_global\",\"stdio\":{\"command\":\"/usr/bin/true\"},\"auth\":{\"strategy\":\"oauth2_token_exchange\"}}"

FIXTURE_REGISTERED=0
case "$RESPONSE_STATUS" in
  201|200)
    say_ok "fixture server created ($RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    FIXTURE_REGISTERED=1
    ;;
  404|405|501)
    say_skip "/api/servers not wired in this build"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit 0
    ;;
  *)
    say_fail "fixture server create returned $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

if [ "$FIXTURE_REGISTERED" = "1" ]; then
  capture_status "GET /v1/servers" \
    -- "$(api_url /v1/servers)"
  case "$RESPONSE_STATUS" in
    200)
      # Substrate fields are additive — assert each on the fixture row
      # so a regression that drops one is caught.
      if printf '%s' "$RESPONSE_BODY" | jq -e '.[] | select(.id == "'"$SUB_ID"'") | .auth_state == "oauth"' >/dev/null 2>&1; then
        say_ok "substrate: auth_state derived ('oauth' for token-exchange)"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "substrate: auth_state missing or wrong"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      if printf '%s' "$RESPONSE_BODY" | jq -e '.[] | select(.id == "'"$SUB_ID"'") | .capabilities' >/dev/null 2>&1; then
        say_ok "substrate: capabilities envelope present"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "substrate: capabilities envelope missing"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      if printf '%s' "$RESPONSE_BODY" | jq -e '.[] | select(.id == "'"$SUB_ID"'") | .policy_state' >/dev/null 2>&1; then
        say_ok "substrate: policy_state present"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "substrate: policy_state missing"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      if printf '%s' "$RESPONSE_BODY" | jq -e '.[] | select(.id == "'"$SUB_ID"'") | .skills_count != null' >/dev/null 2>&1; then
        say_ok "substrate: skills_count present"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "substrate: skills_count missing"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      ;;
    404|405|501)
      say_skip "/v1/servers not wired"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      ;;
    *)
      say_fail "/v1/servers returned $RESPONSE_STATUS"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      ;;
  esac
fi

# 3) /v1/skills carries the skill substrate (assets / status / attached_server).
capture_status "GET /v1/skills" \
  -- "$(api_url /v1/skills)"
case "$RESPONSE_STATUS" in
  200)
    COUNT=$(printf '%s' "$RESPONSE_BODY" | jq '.skills | length' 2>/dev/null || echo 0)
    if [ "${COUNT:-0}" -gt 0 ]; then
      if printf '%s' "$RESPONSE_BODY" | jq -e '.skills[0].assets.prompts != null' >/dev/null 2>&1; then
        say_ok "substrate: skill assets envelope present"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "substrate: skill assets envelope missing"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      if printf '%s' "$RESPONSE_BODY" | jq -e '.skills[0].status != null' >/dev/null 2>&1; then
        say_ok "substrate: skill status present"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "substrate: skill status missing"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
    else
      say_skip "no skills loaded in this build (catalog empty)"
      PHASE_SKIP=$((PHASE_SKIP + 1))
    fi
    ;;
  404|405|501|503)
    say_skip "/v1/skills not wired (HTTP $RESPONSE_STATUS)"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    ;;
  *)
    say_fail "/v1/skills returned $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# Cleanup fixture so reruns stay clean.
if [ "$FIXTURE_REGISTERED" = "1" ]; then
  capture_status "DELETE /api/servers/$SUB_ID" \
    -- -X DELETE "$(api_url /api/servers/$SUB_ID)"
  case "$RESPONSE_STATUS" in
    200|204)
      say_ok "fixture server cleaned up"
      PHASE_OK=$((PHASE_OK + 1))
      ;;
    404|405)
      say_skip "fixture cleanup: route not wired"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      ;;
    *)
      say_warn "fixture cleanup returned $RESPONSE_STATUS (continuing)"
      ;;
  esac
fi

# 4) SPA shells for the redesigned pages serve the embedded console.
# We only verify the HTTP shell (200 + non-empty body); the actual KPI
# strip / filter bar / inspector visibility lives in the Playwright
# specs because those mount post-JS.
for route in /servers /skills /dev/preview; do
  capture_status "GET $route (SPA shell)" -- "$(api_url $route)"
  case "$RESPONSE_STATUS" in
    200)
      if [ -n "${RESPONSE_BODY:-}" ] && printf '%s' "$RESPONSE_BODY" | grep -q '<title>Portico Console'; then
        say_ok "$route shell served"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "$route shell returned 200 but body missing the SPA title"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      ;;
    404|405|501)
      say_skip "$route not served (HTTP $RESPONSE_STATUS)"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      ;;
    *)
      say_fail "$route returned $RESPONSE_STATUS"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      ;;
  esac
done

end_phase
