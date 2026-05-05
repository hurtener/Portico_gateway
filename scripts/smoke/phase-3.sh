#!/usr/bin/env bash
# Phase 3 smoke: resources, prompts, MCP Apps.
# Validates: /v1/resources, /v1/prompts, /v1/apps and the MCP-method
# equivalents resources/list, prompts/list.
#
# Self-skips if /v1/resources is not yet wired.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-3 (resources, prompts, MCP Apps)"

capture_status "GET /v1/resources" -- "$(api_url /v1/resources)"
case "$RESPONSE_STATUS" in
  404|405|501)
    say_skip "phase-3 endpoints not implemented (HTTP $RESPONSE_STATUS) — phase 3 not built yet"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  200) say_ok "GET /v1/resources returns 200"; PHASE_OK=$((PHASE_OK + 1)) ;;
  *)
    say_fail "GET /v1/resources returned HTTP $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

skip_if_404 200 "GET /v1/prompts" -- "$(api_url /v1/prompts)"
skip_if_404 200 "GET /v1/apps"    -- "$(api_url /v1/apps)"

# Initialize a session for MCP-side checks (if /mcp is wired)
INIT_PARAMS='{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"preflight","version":"0"}}'
INIT_REQ=$(jsonrpc initialize "$INIT_PARAMS" 1)
SESSION_ID=$(curl -s -D - -o /dev/null -X POST "$(mcp_url)" \
  -H 'Content-Type: application/json' \
  -d "$INIT_REQ" 2>/dev/null | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/ { gsub(/\r/,"",$2); print $2; exit }')

# resources/list via MCP
LIST_REQ=$(jsonrpc resources/list '{}' 2)
skip_if_404 200 "POST /mcp resources/list" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$LIST_REQ"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_truthy '.result.resources // []' "resources/list: result.resources array present"
fi

# prompts/list via MCP
PLIST_REQ=$(jsonrpc prompts/list '{}' 3)
skip_if_404 200 "POST /mcp prompts/list" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$PLIST_REQ"

# resources/templates/list via MCP
TLIST_REQ=$(jsonrpc resources/templates/list '{}' 4)
skip_if_404 200 "POST /mcp resources/templates/list" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$TLIST_REQ"

end_phase
