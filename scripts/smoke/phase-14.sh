#!/usr/bin/env bash
# Phase 14 smoke: Agent Profiles REST CRUD.
#
# Verifies the /api/agent-profiles surface round-trips: create a profile, read
# it back, see it in the list, delete it, and confirm it's gone. Admin scope is
# required; dev mode grants it. Enforcement (tools/list filtering, LLM alias,
# Skills, snapshot projection), the resolver, CLI, and Console land in later
# units and get their own checks then.
#
# Self-skips when the surface is not built into the running server (404/405/501).

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-14 (agent profiles — REST CRUD)"

# 1) Create a profile.
CREATE_BODY='{"name":"smoke-agent","description":"smoke","allowed_mcp_servers":["github"],"allowed_tools":[],"allowed_skills":[],"allowed_model_aliases":["gpt-4o"],"scopes":["mcp:call"],"enabled":true}'
if skip_if_404 201 "POST /api/agent-profiles" \
  -- -X POST "$(api_url /api/agent-profiles)" \
     -H 'Content-Type: application/json' \
     -d "$CREATE_BODY"; then
  PROFILE_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || true)
  if [ -z "${PROFILE_ID:-}" ] || [ "$PROFILE_ID" = "null" ]; then
    say_fail "create did not return a generated id"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  else
    say_ok "create returned a generated id ($PROFILE_ID)"
    PHASE_OK=$((PHASE_OK + 1))

    # 2) Get it back.
    assert_status 200 "GET /api/agent-profiles/{id}" \
      -- -X GET "$(api_url "/api/agent-profiles/$PROFILE_ID")"
    assert_json_path '.name' 'smoke-agent' "get returns the created profile"
    assert_json_truthy '.allowed_mcp_servers | index("github")' "allowlist round-trips (github present)"

    # 3) List includes it.
    assert_status 200 "GET /api/agent-profiles (list)" \
      -- -X GET "$(api_url /api/agent-profiles)"
    assert_json_truthy "map(.id) | index(\"$PROFILE_ID\")" "list includes the created profile"

    # 3b) JWT binding round-trip: bind a subject, then unbind (both 204).
    assert_status 204 "PUT /api/agent-profiles/{id}/bindings/{sub}" \
      -- -X PUT "$(api_url "/api/agent-profiles/$PROFILE_ID/bindings/smoke-subject")"
    assert_status 204 "DELETE /api/agent-profiles/{id}/bindings/{sub}" \
      -- -X DELETE "$(api_url "/api/agent-profiles/$PROFILE_ID/bindings/smoke-subject")"

    # 4) Delete → 204, then GET → 404.
    assert_status 204 "DELETE /api/agent-profiles/{id}" \
      -- -X DELETE "$(api_url "/api/agent-profiles/$PROFILE_ID")"
    assert_status 404 "GET deleted profile → 404" \
      -- -X GET "$(api_url "/api/agent-profiles/$PROFILE_ID")"
  fi
fi

end_phase
exit $?
