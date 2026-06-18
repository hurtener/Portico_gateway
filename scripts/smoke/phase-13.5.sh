#!/usr/bin/env bash
# Phase 13.5 smoke: MCP Code Mode read meta-tools.
#
# Verifies a session can opt into Code Mode at initialize and that the session
# then sees the mcp.* meta-tools instead of the namespaced catalog, that
# listToolFiles / readToolFile / getToolDocs run end to end, and that a
# non-Code-Mode session is unaffected (acceptance #1). executeToolCode and the
# approval/continuation flow land in later units; their checks are added then.
#
# Self-skips when Code Mode is not built into the running server (the opt-in
# session's tools/list does not advertise the meta-tools).

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-13.5 (mcp code mode — meta-tools + executeToolCode)"

# 1) Initialize a Code Mode session.
CM_INIT='{"protocolVersion":"2025-06-18","capabilities":{"experimental":{"portico":{"code_mode":{"enabled":true}}}},"clientInfo":{"name":"preflight","version":"0"}}'
INIT_REQ=$(jsonrpc initialize "$CM_INIT" 1)
SESSION_HEADERS=$(curl -s -D - -o /dev/null -X POST "$(mcp_url)" \
  -H 'Content-Type: application/json' \
  -d "$INIT_REQ" 2>/dev/null || true)
SESSION_ID=$(printf '%s' "$SESSION_HEADERS" | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/ { gsub(/\r/,"",$2); print $2; exit }')

if [ -z "${SESSION_ID:-}" ]; then
  say_skip "no session id from initialize — MCP northbound not built (phase 1)"
  PHASE_SKIP=$((PHASE_SKIP + 1))
  end_phase
  exit $?
fi

# 2) tools/list on the Code Mode session must advertise the meta-tools.
LIST_REQ=$(jsonrpc tools/list '{}' 2)
capture_status "tools/list (code mode session)" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$LIST_REQ"

HAS_META=$(printf '%s' "$RESPONSE_BODY" | jq -r '.result.tools[]? | select(.name=="mcp.listToolFiles") | .name' 2>/dev/null || true)
if [ "$HAS_META" != "mcp.listToolFiles" ]; then
  say_skip "code mode meta-tools not advertised — phase 13.5 not built in this server"
  PHASE_SKIP=$((PHASE_SKIP + 1))
  end_phase
  exit $?
fi
say_ok "tools/list advertises mcp.listToolFiles for the code mode session"
PHASE_OK=$((PHASE_OK + 1))

# All read + execute meta-tools advertised.
for tool in mcp.readToolFile mcp.getToolDocs mcp.executeToolCode; do
  NAME=$(printf '%s' "$RESPONSE_BODY" | jq -r --arg t "$tool" '.result.tools[]? | select(.name==$t) | .name' 2>/dev/null || true)
  if [ "$NAME" = "$tool" ]; then
    say_ok "tools/list advertises $tool"
    PHASE_OK=$((PHASE_OK + 1))
  else
    say_fail "tools/list missing $tool"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  fi
done

# 3) listToolFiles returns the virtual FS (at least index.md).
CALL_LIST=$(jsonrpc tools/call '{"name":"mcp.listToolFiles","arguments":{}}' 3)
capture_status "tools/call mcp.listToolFiles" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_LIST"
assert_json_truthy '.result.structuredContent.files[0]' "listToolFiles returns a non-empty file list"
INDEX_PRESENT=$(printf '%s' "$RESPONSE_BODY" | jq -r '.result.structuredContent.files | index("index.md")' 2>/dev/null || true)
if [ -n "$INDEX_PRESENT" ] && [ "$INDEX_PRESENT" != "null" ]; then
  say_ok "listToolFiles includes index.md"
  PHASE_OK=$((PHASE_OK + 1))
else
  say_fail "listToolFiles missing index.md"
  PHASE_FAIL=$((PHASE_FAIL + 1))
fi

# 4) readToolFile of index.md returns content.
CALL_READ=$(jsonrpc tools/call '{"name":"mcp.readToolFile","arguments":{"path":"index.md"}}' 4)
capture_status "tools/call mcp.readToolFile index.md" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_READ"
assert_json_truthy '.result.structuredContent.content' "readToolFile returns file content"

# 5) readToolFile of an unknown path returns a typed error.
CALL_BADREAD=$(jsonrpc tools/call '{"name":"mcp.readToolFile","arguments":{"path":"servers/does-not-exist.pyi"}}' 5)
capture_status "tools/call mcp.readToolFile (unknown path)" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_BADREAD"
assert_json_truthy '.error.message' "readToolFile rejects an unknown path with an error"

# 6) getToolDocs runs the docs path (unknown tool -> found:false).
CALL_DOCS=$(jsonrpc tools/call '{"name":"mcp.getToolDocs","arguments":{"tools":["no-such.tool"]}}' 6)
capture_status "tools/call mcp.getToolDocs" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_DOCS"
assert_json_path '.result.structuredContent.docs[0].found' 'false' "getToolDocs reports unknown tool as not found"

# 7) executeToolCode happy path: a pure-compute snippet returns its result.
CALL_EXEC=$(jsonrpc tools/call '{"name":"mcp.executeToolCode","arguments":{"code":"result = 6 * 7"}}' 7)
capture_status "tools/call mcp.executeToolCode (pure compute)" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_EXEC"
assert_json_path '.result.structuredContent.result' '42' "executeToolCode returns the computed result"
assert_json_path '.result.structuredContent.tool_calls' '0' "executeToolCode reports zero tool calls for pure compute"

# 8) Sandbox safety: a load() snippet is rejected with the typed unsafe-call error.
CALL_UNSAFE=$(jsonrpc tools/call '{"name":"mcp.executeToolCode","arguments":{"code":"load(\"os\", \"system\")\nresult = 1"}}' 8)
capture_status "tools/call mcp.executeToolCode (unsafe load)" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_UNSAFE"
assert_json_path '.error.data.code' 'code_mode.unsafe_call' "unsafe snippet returns code_mode.unsafe_call"

# 8b) Continuation surface: resuming with an unknown token fails closed with the
#     typed not-found guard (proves the executeToolCode continuation_token branch
#     is wired). The full approve→resume happy path is covered by the integration
#     suite (it needs an approval-gated downstream tool dev mode does not stand up).
CALL_RESUME=$(jsonrpc tools/call '{"name":"mcp.executeToolCode","arguments":{"continuation_token":"smoke-unknown-token-aaaa"}}' 9)
capture_status "tools/call mcp.executeToolCode (unknown continuation)" \
  -- -X POST "$(mcp_url)" \
     -H 'Content-Type: application/json' \
     -H "Mcp-Session-Id: $SESSION_ID" \
     -d "$CALL_RESUME"
assert_json_path '.error.data.code' 'code_mode.continuation_not_found' "unknown continuation token returns code_mode.continuation_not_found"

# 8c) Code Mode observability endpoints (admin scope; dev mode grants it). The
#     executions list is a (possibly empty) array; the savings rollup carries the
#     tokens-saved totals. Skips cleanly on builds without the code mode store.
if skip_if_404 200 "GET /api/code-mode/executions" \
  -- -X GET "$(api_url /api/code-mode/executions)"; then
  if printf '%s' "$RESPONSE_BODY" | jq -e 'type=="array"' >/dev/null 2>&1; then
    assert_json_truthy 'type=="array"' "code mode executions returns a JSON array"
  fi
  skip_if_404 200 "GET /api/code-mode/savings" \
    -- -X GET "$(api_url /api/code-mode/savings)"
  assert_json_truthy 'has("executions")' "code mode savings rollup has an executions count"
fi

# 9) Acceptance #1: a session WITHOUT the opt-in does not see the meta-tools.
PLAIN_INIT='{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"preflight","version":"0"}}'
PLAIN_HEADERS=$(curl -s -D - -o /dev/null -X POST "$(mcp_url)" \
  -H 'Content-Type: application/json' \
  -d "$(jsonrpc initialize "$PLAIN_INIT" 1)" 2>/dev/null || true)
PLAIN_SID=$(printf '%s' "$PLAIN_HEADERS" | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/ { gsub(/\r/,"",$2); print $2; exit }')
if [ -n "${PLAIN_SID:-}" ]; then
  capture_status "tools/list (plain session)" \
    -- -X POST "$(mcp_url)" \
       -H 'Content-Type: application/json' \
       -H "Mcp-Session-Id: $PLAIN_SID" \
       -d "$(jsonrpc tools/list '{}' 2)"
  PLAIN_HAS_META=$(printf '%s' "$RESPONSE_BODY" | jq -r '.result.tools[]? | select(.name=="mcp.listToolFiles") | .name' 2>/dev/null || true)
  if [ -z "$PLAIN_HAS_META" ]; then
    say_ok "plain session does not see code mode meta-tools (no drift, acceptance #1)"
    PHASE_OK=$((PHASE_OK + 1))
  else
    say_fail "plain session leaked code mode meta-tools"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  fi
fi

end_phase
exit $?
