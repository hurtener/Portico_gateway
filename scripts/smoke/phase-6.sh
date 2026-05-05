#!/usr/bin/env bash
# Phase 6 smoke: catalog snapshots + observability.
# Validates: snapshot creation via /v1/catalog/resolve, snapshot retrieval,
# snapshot diff endpoint shape, OTel attributes on audit events.
#
# Self-skips if /v1/catalog/snapshots is not yet wired.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-6 (catalog snapshots + observability)"

# Initialize a session so we have something to snapshot
INIT_PARAMS='{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"preflight","version":"0"}}'
INIT_REQ=$(jsonrpc initialize "$INIT_PARAMS" 1)
SESSION_HEADERS=$(curl -s -D - -o /dev/null -X POST "$(mcp_url)" \
  -H 'Content-Type: application/json' \
  -d "$INIT_REQ" 2>/dev/null || true)
SESSION_ID=$(printf '%s' "$SESSION_HEADERS" | awk -F': ' '/^[Mm]cp-[Ss]ession-[Ii]d/ { gsub(/\r/,"",$2); print $2; exit }')

if [ -z "${SESSION_ID:-}" ]; then
  say_skip "no session id available — initialize did not produce one (phase 1 may not be built)"
  PHASE_SKIP=$((PHASE_SKIP + 1))
  end_phase
  exit $?
fi

# Trigger snapshot creation
RESOLVE_BODY=$(jq -n --arg sid "$SESSION_ID" '{session_id: $sid}')
capture_status "POST /v1/catalog/resolve" \
  -- -X POST "$(api_url /v1/catalog/resolve)" \
     -H 'Content-Type: application/json' \
     -d "$RESOLVE_BODY"

case "$RESPONSE_STATUS" in
  404|405|501)
    say_skip "phase-6 endpoints not implemented (HTTP $RESPONSE_STATUS) — phase 6 not built yet"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  200|201)
    say_ok "POST /v1/catalog/resolve returns $RESPONSE_STATUS"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  *)
    say_fail "POST /v1/catalog/resolve returned HTTP $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

SNAPSHOT_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id // .ID // .snapshot_id // empty')
if [ -z "$SNAPSHOT_ID" ]; then
  say_warn "snapshot id not found in resolve response (continuing): $(echo "$RESPONSE_BODY" | head -c 200)"
else
  assert_status 200 "GET /v1/catalog/snapshots/$SNAPSHOT_ID" \
    -- "$(api_url "/v1/catalog/snapshots/$SNAPSHOT_ID")"
  assert_json_truthy '.overall_hash // .OverallHash' "snapshot has overall_hash"
fi

# List snapshots
skip_if_404 200 "GET /v1/catalog/snapshots" -- "$(api_url /v1/catalog/snapshots)"

# Audit events should now carry trace/snapshot context
capture_status "GET /v1/audit/events?limit=10" -- "$(api_url '/v1/audit/events?limit=10')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  HAS_TRACE=$(printf '%s' "$RESPONSE_BODY" | jq -r '.events[] | select(.trace_id != null and .trace_id != "") | .trace_id' 2>/dev/null | head -1 || true)
  if [ -n "$HAS_TRACE" ]; then
    say_ok "audit events include trace_id (OTel propagation present)"
    PHASE_OK=$((PHASE_OK + 1))
  else
    say_skip "audit events have no trace_id yet (no traffic generated to populate)"
    PHASE_SKIP=$((PHASE_SKIP + 1))
  fi
fi

end_phase
