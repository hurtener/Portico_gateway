#!/usr/bin/env bash
# Phase 4 smoke: skills runtime + virtual directory.
# Validates: /v1/skills listing, manifest fetch, skill://_index resource.
#
# Self-skips if /v1/skills is not yet wired.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-4 (skills runtime + virtual directory)"

capture_status "GET /v1/skills" -- "$(api_url /v1/skills)"
case "$RESPONSE_STATUS" in
  404|405|501)
    say_skip "phase-4 endpoints not implemented (HTTP $RESPONSE_STATUS) — phase 4 not built yet"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  200) say_ok "GET /v1/skills returns 200"; PHASE_OK=$((PHASE_OK + 1)) ;;
  *)
    say_fail "GET /v1/skills returned HTTP $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

# Pick the first skill (if any) and fetch its manifest
FIRST_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.[0].id // empty' 2>/dev/null || true)
if [ -n "$FIRST_ID" ]; then
  assert_status 200 "GET /v1/skills/$FIRST_ID returns 200" \
    -- "$(api_url "/v1/skills/$FIRST_ID")"
  assert_status 200 "GET /v1/skills/$FIRST_ID/manifest.yaml returns 200" \
    -- "$(api_url "/v1/skills/$FIRST_ID/manifest.yaml")"
else
  say_skip "no skills present in catalog — manifest fetch checks skipped"
  PHASE_SKIP=$((PHASE_SKIP + 2))
fi

# skill://_index virtual resource via MCP resources/read
INIT_PARAMS='{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"preflight","version":"0"}}'
INIT_REQ=$(jsonrpc initialize "$INIT_PARAMS" 1)
SESSION_ID=$(curl -s -D - -o /dev/null -X POST "$(mcp_url)" \
  -H 'Content-Type: application/json' \
  -d "$INIT_REQ" 2>/dev/null | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/ { gsub(/\r/,"",$2); print $2; exit }')

INDEX_REQ=$(jsonrpc resources/read '{"uri":"skill://_index"}' 2)
skip_if_404 200 "POST /mcp resources/read skill://_index" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$INDEX_REQ"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_truthy '.result.contents' "skill://_index: result.contents present"
fi

end_phase
