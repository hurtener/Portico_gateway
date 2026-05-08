#!/usr/bin/env bash
# Phase 10 smoke: Interactive MCP Playground.
#
# Exercises the synthetic-JWT session bootstrap, snapshot-bound catalog
# fetch, ad-hoc tool call (SSE chunked), correlation pull, saved-case
# CRUD, replay, and the carry-over log-tail SSE on /api/servers/{id}/logs.
#
# Dev-mode synth tenant carries `admin` so every endpoint is reachable
# without a JWT. Phase 9 prerequisites (servers + cases stores) must be
# wired or the script SKIPs cleanly.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-10 (interactive MCP playground)"

# 1) Start a playground session.
SESSION_PAYLOAD='{}'
capture_status "POST /api/playground/sessions" \
  -- -X POST "$(api_url /api/playground/sessions)" \
  -H 'Content-Type: application/json' \
  -d "$SESSION_PAYLOAD"
case "$RESPONSE_STATUS" in
  201|200)
    say_ok "session bootstrap ($RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    SID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || echo "")
    TOKEN=$(printf '%s' "$RESPONSE_BODY" | jq -r '.token' 2>/dev/null || echo "")
    if [ -z "$SID" ] || [ "$SID" = "null" ]; then
      say_fail "session id missing from response"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      end_phase
      exit $?
    fi
    if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
      say_fail "session token missing from response"
      PHASE_FAIL=$((PHASE_FAIL + 1))
    else
      # Token must be RS256-shaped (3 dots-separated parts).
      if [ "$(echo "$TOKEN" | tr -cd '.' | wc -c | tr -d ' ')" = "2" ]; then
        say_ok "token is JWT-shaped"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "token does not look JWT-shaped: $TOKEN"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
    fi
    ;;
  404|405|501|503)
    say_skip "session endpoint not wired ($RESPONSE_STATUS)"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  *)
    say_fail "session bootstrap $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

# 2) Catalog fetch — snapshot-bound.
skip_if_404 200 "GET /api/playground/sessions/{sid}/catalog" \
  -- "$(api_url /api/playground/sessions/$SID/catalog)"

# 3) Issue a tool call (synthetic — adapter records start/complete audits).
CALL_PAYLOAD='{"kind":"tool_call","target":"smoke.example","arguments":{}}'
capture_status "POST /api/playground/sessions/{sid}/calls" \
  -- -X POST "$(api_url /api/playground/sessions/$SID/calls)" \
  -H 'Content-Type: application/json' \
  -d "$CALL_PAYLOAD"
case "$RESPONSE_STATUS" in
  202|200)
    say_ok "call enqueued ($RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    CID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.call_id' 2>/dev/null || echo "")
    ;;
  404|405|501|503)
    say_skip "calls not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    CID=""
    ;;
  *)
    say_fail "call $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    CID=""
    ;;
esac

# 4) Stream the call (SSE). Use --max-time to bound the connection.
if [ -n "$CID" ]; then
  STREAM_TMP=$(mktemp -t portico-stream.XXXXXX)
  STREAM_STATUS=$(curl -s --max-time 3 -o "$STREAM_TMP" -w "%{http_code}" \
    "$(api_url /api/playground/sessions/$SID/calls/$CID/stream)" \
    -H 'Accept: text/event-stream' 2>/dev/null || true)
  STREAM_BODY=$(cat "$STREAM_TMP" || true)
  rm -f "$STREAM_TMP"
  case "${STREAM_STATUS:0:3}" in
    200)
      # Accept either an `event: chunk` (happy path against a real
      # downstream MCP server) or an `event: error` envelope (Phase 10.5
      # routes through the real dispatcher; a synthetic `smoke.example`
      # target legitimately resolves to "unknown server"). Both prove
      # the SSE handshake + dispatcher wiring.
      if printf '%s' "$STREAM_BODY" | grep -qE "^event: (chunk|error)"; then
        say_ok "SSE frame received from dispatcher"
        PHASE_OK=$((PHASE_OK + 1))
      else
        say_fail "expected 'event: chunk' or 'event: error' in stream body"
        PHASE_FAIL=$((PHASE_FAIL + 1))
      fi
      ;;
    404|405|501|503)
      say_skip "stream not wired"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      ;;
    *)
      say_fail "stream status=$STREAM_STATUS"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      ;;
  esac
fi

# 5) Correlation pull.
skip_if_404 200 "GET /api/playground/sessions/{sid}/correlation" \
  -- "$(api_url /api/playground/sessions/$SID/correlation)"

# 6) Save a case.
CASE_PAYLOAD='{"name":"smoke happy path","kind":"tool_call","target":"smoke.example","payload":{"name":"smoke.example","arguments":{}},"tags":["smoke"]}'
capture_status "POST /api/playground/cases" \
  -- -X POST "$(api_url /api/playground/cases)" \
  -H 'Content-Type: application/json' \
  -d "$CASE_PAYLOAD"
case "$RESPONSE_STATUS" in
  201|200)
    say_ok "case saved"
    PHASE_OK=$((PHASE_OK + 1))
    CASE_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || echo "")
    ;;
  404|405|501|503)
    say_skip "case save not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    CASE_ID=""
    ;;
  *)
    say_fail "case save $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    CASE_ID=""
    ;;
esac

# 7) List cases — must include our newly saved one.
skip_if_404 200 "GET /api/playground/cases" -- "$(api_url /api/playground/cases)"

# 8) Replay the case.
if [ -n "$CASE_ID" ]; then
  capture_status "POST /api/playground/cases/{id}/replay" \
    -- -X POST "$(api_url /api/playground/cases/$CASE_ID/replay)"
  case "$RESPONSE_STATUS" in
    202|200)
      say_ok "replay accepted"
      PHASE_OK=$((PHASE_OK + 1))
      RUN_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || echo "")
      ;;
    404|405|501|503)
      say_skip "replay not wired"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      RUN_ID=""
      ;;
    *)
      say_fail "replay $RESPONSE_STATUS body=$RESPONSE_BODY"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      RUN_ID=""
      ;;
  esac
fi

# 9) Run detail.
if [ -n "${RUN_ID:-}" ]; then
  skip_if_404 200 "GET /api/playground/runs/{run_id}" \
    -- "$(api_url /api/playground/runs/$RUN_ID)"
fi

# 10) Case run history.
if [ -n "$CASE_ID" ]; then
  skip_if_404 200 "GET /api/playground/cases/{id}/runs" \
    -- "$(api_url /api/playground/cases/$CASE_ID/runs)"
fi

# 11) Carry-over: log-tail SSE on /api/servers/{id}/logs.
# First create a server to attach a ring to.
SERVER_PAYLOAD='{"id":"smoke-pg-srv","display_name":"Smoke","transport":"stdio","stdio":{"command":"/bin/true"}}'
capture_status "POST /api/servers (for log tail)" \
  -- -X POST "$(api_url /api/servers)" \
  -H 'Content-Type: application/json' \
  -d "$SERVER_PAYLOAD" >/dev/null 2>&1 || true

# Hit the SSE endpoint with --max-time so the script doesn't hang.
# SSE responses don't terminate cleanly inside --max-time, so the
# capture_status status often becomes a junk concatenation. Run curl
# directly into a tmpfile and accept "200" or any string starting with
# "200" as a success signal.
LOG_TMP=$(mktemp -t portico-logs.XXXXXX)
LOG_STATUS=$(curl -s --max-time 3 -o "$LOG_TMP" -w "%{http_code}" \
  "$(api_url /api/servers/smoke-pg-srv/logs)" \
  -H 'Accept: text/event-stream' 2>/dev/null || true)
LOG_BODY=$(cat "$LOG_TMP" || true)
rm -f "$LOG_TMP"
case "${LOG_STATUS:0:3}" in
  200)
    if printf '%s' "$LOG_BODY" | grep -q ": connected"; then
      say_ok "log SSE connected"
      PHASE_OK=$((PHASE_OK + 1))
    else
      say_fail "expected ': connected' in SSE body"
      PHASE_FAIL=$((PHASE_FAIL + 1))
    fi
    ;;
  404|405|501)
    say_skip "log SSE not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    ;;
  *)
    say_fail "log SSE status=$LOG_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 12) Delete the case (cleanup).
if [ -n "$CASE_ID" ]; then
  capture_status "DELETE /api/playground/cases/{id}" \
    -- -X DELETE "$(api_url /api/playground/cases/$CASE_ID)"
  case "$RESPONSE_STATUS" in
    204|200)
      say_ok "case deleted"
      PHASE_OK=$((PHASE_OK + 1))
      ;;
    404|405|501|503)
      say_skip "delete not wired"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      ;;
    *)
      say_fail "delete $RESPONSE_STATUS"
      PHASE_FAIL=$((PHASE_FAIL + 1))
      ;;
  esac
fi

# 13) End the session.
capture_status "DELETE /api/playground/sessions/{sid}" \
  -- -X DELETE "$(api_url /api/playground/sessions/$SID)"
case "$RESPONSE_STATUS" in
  204|200)
    say_ok "session ended"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  404|405|501|503)
    say_skip "session end not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    ;;
  *)
    say_fail "session end $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

end_phase
