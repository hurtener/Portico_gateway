#!/usr/bin/env bash
# Phase 16 smoke: A2A peers (REST CRUD).
#
# Verifies the /api/a2a/peers surface: register a peer, read it back, list,
# update (rename), and delete. Admin scope is required; dev mode grants it.
# Agent-card ingestion + health/refresh-card land in a later unit and get
# their own checks then.
#
# Self-skips when the surface is not built into the running server (404/405/501).

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-16 (a2a peers)"

# 1) Register an A2A peer. admin scope (dev mode grants it automatically),
# so no Authorization header is needed.
if skip_if_404 201 "POST /api/a2a/peers" \
  -- -X POST "$(api_url /api/a2a/peers)" \
     -H 'Content-Type: application/json' \
     -d '{"name":"smoke-peer","endpoint":"https://peer.example/a2a"}'; then
  PEER_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || true)
  if [ -z "${PEER_ID:-}" ] || [ "$PEER_ID" = "null" ]; then
    say_fail "create did not return an a2a peer id"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  else
    say_ok "create returned an a2a peer id ($PEER_ID)"
    PHASE_OK=$((PHASE_OK + 1))

    assert_status 200 "GET /api/a2a/peers/{id}" \
      -- -X GET "$(api_url "/api/a2a/peers/$PEER_ID")"
    assert_json_path '.name' 'smoke-peer' "get returns the created peer"

    assert_status 200 "GET /api/a2a/peers (list)" \
      -- -X GET "$(api_url /api/a2a/peers)"
    assert_json_truthy "map(.id) | index(\"$PEER_ID\")" "list includes the created peer"

    assert_status 200 "PUT /api/a2a/peers/{id}" \
      -- -X PUT "$(api_url "/api/a2a/peers/$PEER_ID")" \
         -H 'Content-Type: application/json' \
         -d '{"name":"smoke-peer-2","endpoint":"https://peer.example/a2a"}'
    assert_json_path '.name' 'smoke-peer-2' "update renamed the peer"

    assert_status 204 "DELETE /api/a2a/peers/{id}" \
      -- -X DELETE "$(api_url "/api/a2a/peers/$PEER_ID")"
  fi
fi

end_phase
exit $?
