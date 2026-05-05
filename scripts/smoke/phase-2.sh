#!/usr/bin/env bash
# Phase 2 smoke: registry + lifecycle.
# Validates: /v1/servers CRUD, hot reload trigger, instance listing.
#
# Self-skips if /v1/servers is not yet wired.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-2 (registry + lifecycle)"

# Probe: is /v1/servers wired?
capture_status "GET /v1/servers" -- "$(api_url /v1/servers)"
case "$RESPONSE_STATUS" in
  404|405|501)
    say_skip "phase-2 endpoints not implemented (HTTP $RESPONSE_STATUS) — phase 2 not built yet"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  200) say_ok "GET /v1/servers returns 200"; PHASE_OK=$((PHASE_OK + 1)) ;;
  *)
    say_fail "GET /v1/servers returned HTTP $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    end_phase
    exit $?
    ;;
esac

# Create a server (using mockmcp if present, else a fake remote_static URL that won't ever be called)
SERVER_ID="preflight-test-server"
if [ -x ./bin/mockmcp ]; then
  SPEC=$(jq -n --arg cmd "$PWD/bin/mockmcp" '{
    id: "'"$SERVER_ID"'",
    display_name: "Preflight Mock",
    transport: "stdio",
    runtime_mode: "shared_global",
    stdio: { command: $cmd, args: [] },
    health: { ping_interval: "0s", ping_timeout: "5s", startup_grace: "5s" },
    lifecycle: { idle_timeout: "0s" }
  }')
else
  SPEC=$(jq -n '{
    id: "'"$SERVER_ID"'",
    display_name: "Preflight Mock",
    transport: "http",
    runtime_mode: "remote_static",
    http: { url: "http://127.0.0.1:1/never-called", timeout: "1s" }
  }')
fi

assert_status 201 "POST /v1/servers creates a server" \
  -- -X POST "$(api_url /v1/servers)" \
     -H 'Content-Type: application/json' \
     -d "$SPEC"

assert_status 200 "GET /v1/servers/{id} returns the new server" \
  -- "$(api_url /v1/servers/$SERVER_ID)"
assert_json_path '.id' "$SERVER_ID" "server detail body has correct id"

# Reload (should be 202 Accepted)
skip_if_404 202 "POST /v1/servers/{id}/reload returns 202" \
  -- -X POST "$(api_url /v1/servers/$SERVER_ID/reload)"

# Disable
skip_if_404 200 "POST /v1/servers/{id}/disable" \
  -- -X POST "$(api_url /v1/servers/$SERVER_ID/disable)"

# Instances list
skip_if_404 200 "GET /v1/servers/{id}/instances" \
  -- "$(api_url /v1/servers/$SERVER_ID/instances)"

# Cleanup
assert_status 204 "DELETE /v1/servers/{id} removes the server" \
  -- -X DELETE "$(api_url /v1/servers/$SERVER_ID)"

end_phase
