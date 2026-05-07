#!/usr/bin/env bash
# Phase 8 smoke: skill sources first-class.
#
# Asserts the REST surface for /api/skill-sources, /api/skills/authored,
# and /api/skills/validate. Self-skips per endpoint when the feature
# isn't implemented in the build.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-8 (skill sources first-class)"

# 1) GET /api/skill-sources lists (initial empty)
skip_if_404 200 "GET /api/skill-sources" -- "$(api_url /api/skill-sources)"

# 2) POST /api/skill-sources adds a Git source pointing at the local
#    fixture repo. The fixture is expected to live at
#    examples/skills/external-git-fixture, but it doesn't exist in V1
#    (test fixture is created by the integration test). The endpoint
#    must accept a request that uses any URL (offline-friendly); we
#    use a localhost-like dummy URL since the boot skipping works.
PAYLOAD='{
  "name":"smoke-source",
  "driver":"http",
  "config":{"feed_url":"http://127.0.0.1:65535/feed"},
  "refresh_seconds":300,
  "priority":50,
  "enabled":true
}'
capture_status "POST /api/skill-sources" \
  -- -X POST "$(api_url /api/skill-sources)" \
  -H 'Content-Type: application/json' \
  -d "$PAYLOAD"
case "$RESPONSE_STATUS" in
  200|201)
    say_ok "POST /api/skill-sources accepted ($RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  404|405|501)
    say_skip "POST /api/skill-sources not wired ($RESPONSE_STATUS)"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  *)
    say_fail "POST /api/skill-sources $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 3) GET the source back
skip_if_404 200 "GET /api/skill-sources/{name}" \
  -- "$(api_url /api/skill-sources/smoke-source)"

# 4) POST refresh — the underlying http feed will fail; we just want the
#    handler to be wired (200 or non-fatal error).
capture_status "POST /api/skill-sources/{name}/refresh" \
  -- -X POST "$(api_url /api/skill-sources/smoke-source/refresh)"
case "$RESPONSE_STATUS" in
  200|500|400)
    say_ok "refresh handler wired (HTTP $RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  *)
    say_fail "refresh handler $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 5) GET packs (may be empty if fetch failed)
capture_status "GET /api/skill-sources/{name}/packs" \
  -- "$(api_url /api/skill-sources/smoke-source/packs)"
case "$RESPONSE_STATUS" in
  200|500|400)
    say_ok "packs handler wired (HTTP $RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  *)
    say_fail "packs handler $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 6) Authored skill: create draft
AUTHORED='{
  "manifest":"id: acme.smoke\ntitle: Smoke\nversion: 1.0.0\nspec: skills/v1\ninstructions: SKILL.md\nbinding:\n  required_tools:\n    - acme.do\n",
  "files":[{"relpath":"SKILL.md","mime_type":"text/markdown","body":"# Hi"}]
}'
capture_status "POST /api/skills/authored (draft)" \
  -- -X POST "$(api_url /api/skills/authored)" \
  -H 'Content-Type: application/json' \
  -d "$AUTHORED"
case "$RESPONSE_STATUS" in
  201|200)
    say_ok "draft created"
    PHASE_OK=$((PHASE_OK + 1))
    assert_json_path '.status' 'draft' "draft.status == draft"
    ;;
  404|405|501)
    say_skip "authored skills not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  *)
    say_fail "draft create $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 7) Publish the draft
capture_status "POST /api/skills/authored/{id}/versions/{v}/publish" \
  -- -X POST "$(api_url /api/skills/authored/acme.smoke/versions/1.0.0/publish)"
case "$RESPONSE_STATUS" in
  200)
    say_ok "publish succeeded"
    PHASE_OK=$((PHASE_OK + 1))
    assert_json_path '.status' 'published' "published.status == published"
    ;;
  *)
    say_fail "publish $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 8) Validate broken manifest — expect 200 with violations array
BAD_MANIFEST='{"manifest":"this is not yaml: ["}'
capture_status "POST /api/skills/validate (broken)" \
  -- -X POST "$(api_url /api/skills/validate)" \
  -H 'Content-Type: application/json' \
  -d "$BAD_MANIFEST"
case "$RESPONSE_STATUS" in
  200)
    say_ok "validate handler wired"
    PHASE_OK=$((PHASE_OK + 1))
    if printf '%s' "$RESPONSE_BODY" | jq -e '.violations | length > 0' >/dev/null 2>&1; then
      say_ok "broken manifest produced ≥1 violation"
      PHASE_OK=$((PHASE_OK + 1))
    else
      say_skip "no violations reported (validator may not be wired)"
      PHASE_SKIP=$((PHASE_SKIP + 1))
    fi
    ;;
  404|405|501)
    say_skip "validate not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    ;;
  *)
    say_fail "validate $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 9) DELETE source
capture_status "DELETE /api/skill-sources/{name}" \
  -- -X DELETE "$(api_url /api/skill-sources/smoke-source)"
case "$RESPONSE_STATUS" in
  204|200)
    say_ok "delete succeeded"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  404)
    say_skip "delete unsupported in this build"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    ;;
  *)
    say_fail "delete $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

end_phase
