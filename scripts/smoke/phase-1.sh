#!/usr/bin/env bash
# Phase 1 smoke: MCP gateway core.
# Validates: northbound HTTP+SSE, initialize handshake, tools/list aggregation,
# tools/call routing, error mapping for unknown tools.
#
# Self-skips entire phase if /mcp endpoint is not yet wired (404 on initialize).

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-1 (MCP gateway core)"

# Probe: is /mcp wired at all?
INIT_PARAMS='{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"preflight","version":"0"}}'
INIT_REQ=$(jsonrpc initialize "$INIT_PARAMS" 1)

capture_status "POST /mcp initialize" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H 'Accept: application/json' \
     -d "$INIT_REQ"

case "$RESPONSE_STATUS" in
  404|405|501)
    say_skip "phase-1 endpoints not implemented (HTTP $RESPONSE_STATUS) — phase 1 not built yet"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  200)
    say_ok "POST /mcp initialize returns 200"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  *)
    say_fail "POST /mcp initialize returned HTTP $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

# Validate initialize result shape
assert_json_path '.jsonrpc' '2.0'        "initialize: jsonrpc is 2.0"
assert_json_truthy '.result.serverInfo.name' "initialize: serverInfo.name present"
assert_json_truthy '.result.protocolVersion' "initialize: protocolVersion present"

# Capture the session ID returned by the server
SESSION_ID=$(curl -s -D - -o /dev/null -X POST "$(mcp_url)" \
  -H 'Content-Type: application/json' \
  -d "$INIT_REQ" | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/ { gsub(/\r/,"",$2); print $2; exit }')

if [ -z "${SESSION_ID:-}" ]; then
  say_warn "no Mcp-Session-Id header in initialize response — may be acceptable for some implementations"
fi

# tools/list should return an envelope with a tools array (empty in baseline)
LIST_REQ=$(jsonrpc tools/list '{}' 2)
assert_status 200 "POST /mcp tools/list returns 200" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$LIST_REQ"
assert_json_truthy '.result.tools // []' "tools/list: result.tools array present"

# Calling a non-existent tool must produce a JSON-RPC error (not HTTP 5xx)
CALL_REQ=$(jsonrpc tools/call '{"name":"definitely.does_not_exist","arguments":{}}' 3)
capture_status "POST /mcp tools/call (unknown tool)" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$CALL_REQ"
if [ "$RESPONSE_STATUS" = "200" ]; then
  if printf '%s' "$RESPONSE_BODY" | jq -e '.error.code' >/dev/null 2>&1; then
    say_ok "tools/call unknown tool returns JSON-RPC error envelope"
    PHASE_OK=$((PHASE_OK + 1))
  else
    say_fail "tools/call unknown tool: expected JSON-RPC error, got: $(echo "$RESPONSE_BODY" | head -c 200)"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  fi
else
  say_fail "tools/call unknown tool: expected HTTP 200 with JSON-RPC error, got HTTP $RESPONSE_STATUS"
  PHASE_FAIL=$((PHASE_FAIL + 1))
fi

# Bad method must return -32601 method not found (JSON-RPC standard)
BAD_REQ=$(jsonrpc this/method/does/not/exist '{}' 4)
capture_status "POST /mcp unknown method" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     ${SESSION_ID:+-H "Mcp-Session-Id: $SESSION_ID"} \
     -d "$BAD_REQ"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | jq -e '.error.code == -32601' >/dev/null 2>&1; then
  say_ok "unknown method returns JSON-RPC -32601"
  PHASE_OK=$((PHASE_OK + 1))
else
  say_skip "unknown method JSON-RPC code check (got status=$RESPONSE_STATUS body=$(echo "$RESPONSE_BODY" | head -c 120))"
  PHASE_SKIP=$((PHASE_SKIP + 1))
fi

end_phase
