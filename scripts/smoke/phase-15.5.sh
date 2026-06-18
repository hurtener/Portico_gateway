#!/usr/bin/env bash
# Phase 15.5 smoke: governance Virtual Keys (REST CRUD + bearer auth).
#
# Verifies the /api/governance/virtual-keys surface: create a VK (secret shown
# once), read it back without the secret, list it, authenticate with the VK
# bearer ("pk-portico-…"), reject a garbage VK token (401 vk_unknown), rotate
# (old token stops working), and revoke (401 vk_revoked). Admin scope is
# required; dev mode grants it. Semantic cache + budget tiers land in later
# units and get their own checks then.
#
# Self-skips when the surface is not built into the running server (404/405/501).

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-15.5 (governance virtual keys)"

# 1) Create a VK (admin scope so the same token can call governance endpoints).
CREATE_BODY='{"name":"smoke-vk","scopes":["admin","llm:invoke"],"provider_allowlist":[],"model_allowlist":[],"mcp_server_allowlist":[]}'
if skip_if_404 201 "POST /api/governance/virtual-keys" \
  -- -X POST "$(api_url /api/governance/virtual-keys)" \
     -H 'Content-Type: application/json' \
     -d "$CREATE_BODY"; then
  VK_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.virtual_key.id' 2>/dev/null || true)
  VK_TOKEN=$(printf '%s' "$RESPONSE_BODY" | jq -r '.token' 2>/dev/null || true)

  if [ -z "${VK_ID:-}" ] || [ "$VK_ID" = "null" ]; then
    say_fail "create did not return a virtual key id"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  else
    say_ok "create returned a virtual key id ($VK_ID)"
    PHASE_OK=$((PHASE_OK + 1))

    # Token must be present and prefixed pk-portico-.
    case "$VK_TOKEN" in
      pk-portico-*)
        say_ok "create returned a pk-portico- token (shown once)"
        PHASE_OK=$((PHASE_OK + 1)) ;;
      *)
        say_fail "create did not return a pk-portico- token"
        PHASE_FAIL=$((PHASE_FAIL + 1)) ;;
    esac

    # 2) Get it back — the secret must NOT be present.
    assert_status 200 "GET /api/governance/virtual-keys/{id}" \
      -- -X GET "$(api_url "/api/governance/virtual-keys/$VK_ID")"
    assert_json_path '.name' 'smoke-vk' "get returns the created VK"
    if printf '%s' "$RESPONSE_BODY" | jq -e 'has("token") or has("secret")' >/dev/null 2>&1; then
      say_fail "get response leaked a token/secret field"
      PHASE_FAIL=$((PHASE_FAIL + 1))
    else
      say_ok "get response carries no secret"
      PHASE_OK=$((PHASE_OK + 1))
    fi

    # 3) List includes it.
    assert_status 200 "GET /api/governance/virtual-keys (list)" \
      -- -X GET "$(api_url /api/governance/virtual-keys)"
    assert_json_truthy "map(.id) | index(\"$VK_ID\")" "list includes the created VK"

    # 3b) Hierarchical budget read for the VK (200 with a levels array; empty
    # when no budgets are attached — still a valid response shape).
    assert_status 200 "GET /api/governance/virtual-keys/{id}/budget" \
      -- -X GET "$(api_url "/api/governance/virtual-keys/$VK_ID/budget")"
    assert_json_truthy '.levels | type == "array"' "budget read returns a levels array"

    # 4) Authenticate WITH the VK bearer (admin scope → governance allowed).
    assert_status 200 "VK bearer authenticates (list via pk-portico-)" \
      -- -X GET "$(api_url /api/governance/virtual-keys)" \
         -H "Authorization: Bearer $VK_TOKEN"

    # 5) A malformed VK token is rejected (401, never authenticated).
    assert_status 401 "garbage VK token → 401" \
      -- -X GET "$(api_url /api/governance/virtual-keys)" \
         -H "Authorization: Bearer pk-portico-vk_deadbeef.notarealsecret"

    # 6) Rotate → new token; the OLD token stops authenticating.
    if assert_status 200 "POST /api/governance/virtual-keys/{id}/rotate" \
      -- -X POST "$(api_url "/api/governance/virtual-keys/$VK_ID/rotate")"; then
      NEW_TOKEN=$(printf '%s' "$RESPONSE_BODY" | jq -r '.token' 2>/dev/null || true)
      assert_status 401 "old VK token rejected after rotation" \
        -- -X GET "$(api_url /api/governance/virtual-keys)" \
           -H "Authorization: Bearer $VK_TOKEN"
      assert_status 200 "new VK token authenticates after rotation" \
        -- -X GET "$(api_url /api/governance/virtual-keys)" \
           -H "Authorization: Bearer $NEW_TOKEN"
      VK_TOKEN="$NEW_TOKEN"
    fi

    # 7) Revoke (DELETE) → the token is no longer accepted.
    assert_status 204 "DELETE /api/governance/virtual-keys/{id} (revoke)" \
      -- -X DELETE "$(api_url "/api/governance/virtual-keys/$VK_ID")"
    assert_status 401 "revoked VK token rejected" \
      -- -X GET "$(api_url /api/governance/virtual-keys)" \
         -H "Authorization: Bearer $VK_TOKEN"
  fi
fi

# 8) Governance Customer CRUD (admin scope; dev mode grants it).
CUST_BODY='{"name":"smoke-cust","description":"smoke customer","webhook_url":"https://smoke.example/hook"}'
if skip_if_404 201 "POST /api/governance/customers" \
  -- -X POST "$(api_url /api/governance/customers)" \
     -H 'Content-Type: application/json' -d "$CUST_BODY"; then
  CUST_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || true)
  if [ -z "${CUST_ID:-}" ] || [ "$CUST_ID" = "null" ]; then
    say_fail "customer create did not return an id"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  else
    say_ok "customer create returned an id ($CUST_ID)"
    PHASE_OK=$((PHASE_OK + 1))
    assert_status 200 "GET /api/governance/customers/{id}" \
      -- -X GET "$(api_url "/api/governance/customers/$CUST_ID")"
    assert_json_path '.name' 'smoke-cust' "get returns the created customer"
    assert_status 200 "GET /api/governance/customers (list)" \
      -- -X GET "$(api_url /api/governance/customers)"
    assert_json_truthy "map(.id) | index(\"$CUST_ID\")" "list includes the created customer"
    assert_status 204 "DELETE /api/governance/customers/{id}" \
      -- -X DELETE "$(api_url "/api/governance/customers/$CUST_ID")"
  fi
fi

# 9) Governance Team CRUD. Standalone team (no customer link) so the customer
# delete above does not break the team FK (linkage is covered by unit tests).
TEAM_BODY='{"name":"smoke-team","description":"smoke team"}'
if skip_if_404 201 "POST /api/governance/teams" \
  -- -X POST "$(api_url /api/governance/teams)" \
     -H 'Content-Type: application/json' -d "$TEAM_BODY"; then
  TEAM_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || true)
  if [ -z "${TEAM_ID:-}" ] || [ "$TEAM_ID" = "null" ]; then
    say_fail "team create did not return an id"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  else
    say_ok "team create returned an id ($TEAM_ID)"
    PHASE_OK=$((PHASE_OK + 1))
    assert_status 200 "GET /api/governance/teams/{id}" \
      -- -X GET "$(api_url "/api/governance/teams/$TEAM_ID")"
    assert_json_path '.name' 'smoke-team' "get returns the created team"
    assert_status 200 "GET /api/governance/teams (list)" \
      -- -X GET "$(api_url /api/governance/teams)"
    assert_json_truthy "map(.id) | index(\"$TEAM_ID\")" "list includes the created team"
    assert_status 204 "DELETE /api/governance/teams/{id}" \
      -- -X DELETE "$(api_url "/api/governance/teams/$TEAM_ID")"
  fi
fi

# 10) Governance Budget CRUD. scope_kind=tenant, scope_id=dev tenant.
BUDGET_BODY='{"scope_kind":"tenant","scope_id":"dev","metric":"cost_usd","period":"1d","limit_val":5.0,"enabled":true}'
if skip_if_404 201 "POST /api/governance/budgets" \
  -- -X POST "$(api_url /api/governance/budgets)" \
     -H 'Content-Type: application/json' -d "$BUDGET_BODY"; then
  BUDGET_ID=$(printf '%s' "$RESPONSE_BODY" | jq -r '.id' 2>/dev/null || true)
  if [ -z "${BUDGET_ID:-}" ] || [ "$BUDGET_ID" = "null" ]; then
    say_fail "budget create did not return an id"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  else
    say_ok "budget create returned an id ($BUDGET_ID)"
    PHASE_OK=$((PHASE_OK + 1))
    assert_status 200 "GET /api/governance/budgets/{id}" \
      -- -X GET "$(api_url "/api/governance/budgets/$BUDGET_ID")"
    assert_json_path '.metric' 'cost_usd' "get returns the created budget"
    assert_status 200 "GET /api/governance/budgets (list)" \
      -- -X GET "$(api_url /api/governance/budgets)"
    assert_json_truthy "map(.id) | index(\"$BUDGET_ID\")" "list includes the created budget"
    assert_status 204 "DELETE /api/governance/budgets/{id}" \
      -- -X DELETE "$(api_url "/api/governance/budgets/$BUDGET_ID")"
  fi
fi

# 11) Semantic cache admin surface. config/stats are always available (report
# "none" when no cache is configured — dev defaults to no cache, so a 200 with
# driver:"none" is expected). invalidate requires a live cache (503 when none).
assert_status 200 "GET /api/llm/cache/config" \
  -- -X GET "$(api_url /api/llm/cache/config)"
assert_json_truthy '.driver' "cache config reports a driver"
assert_status 200 "GET /api/llm/cache/stats" \
  -- -X GET "$(api_url /api/llm/cache/stats)"
# invalidate: 200 when a cache is configured, 503 when not — both are acceptable
# (the dev server defaults to no cache). Treat 503 as a skip via skip_if_404's
# sibling: assert it is one of the two by checking it is not a hard failure.
INVAL_STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$(api_url /api/llm/cache/invalidate)" \
  -H 'Content-Type: application/json' -d '{"all":true}')
if [ "$INVAL_STATUS" = "200" ] || [ "$INVAL_STATUS" = "503" ]; then
  say_ok "POST /api/llm/cache/invalidate (HTTP $INVAL_STATUS — 200 cached / 503 no-cache)"
  PHASE_OK=$((PHASE_OK + 1))
else
  say_fail "POST /api/llm/cache/invalidate unexpected HTTP $INVAL_STATUS"
  PHASE_FAIL=$((PHASE_FAIL + 1))
fi

end_phase
exit $?
