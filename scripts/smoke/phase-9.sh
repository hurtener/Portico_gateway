#!/usr/bin/env bash
# Phase 9 smoke: Console CRUD for servers, tenants, secrets, policy.
#
# Exercises the new /api/* surface added by Phase 9. The dev mode synth
# tenant identity carries the `admin` scope so every endpoint is reachable
# without a JWT.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-9 (console CRUD)"

# 1) GET /api/servers (list — same data as /v1/servers)
skip_if_404 200 "GET /api/servers" -- "$(api_url /api/servers)"

# 2) POST /api/servers — register a fresh server
SERVER_PAYLOAD='{
  "id":"smoke-srv",
  "display_name":"Smoke",
  "transport":"stdio",
  "stdio":{"command":"/bin/true"}
}'
capture_status "POST /api/servers" \
  -- -X POST "$(api_url /api/servers)" \
  -H 'Content-Type: application/json' \
  -d "$SERVER_PAYLOAD"
case "$RESPONSE_STATUS" in
  201|200)
    say_ok "POST /api/servers accepted ($RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    ;;
  404|405|501)
    say_skip "POST /api/servers not wired"
    PHASE_SKIP=$((PHASE_SKIP + 1))
    end_phase
    exit $?
    ;;
  *)
    say_fail "POST /api/servers $RESPONSE_STATUS body=$RESPONSE_BODY"
    PHASE_FAIL=$((PHASE_FAIL + 1))
    ;;
esac

# 3) GET the server back
skip_if_404 200 "GET /api/servers/{id}" -- "$(api_url /api/servers/smoke-srv)"

# 4) PATCH partial update — toggle enabled
capture_status "PATCH /api/servers/{id}" \
  -- -X PATCH "$(api_url /api/servers/smoke-srv)" \
  -H 'Content-Type: application/json' \
  -d '{"enabled":false}'
case "$RESPONSE_STATUS" in
  200) say_ok "PATCH server enabled=false"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "PATCH not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "PATCH $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 5) Restart endpoint
capture_status "POST /api/servers/{id}/restart" \
  -- -X POST "$(api_url /api/servers/smoke-srv/restart)" \
  -H 'Content-Type: application/json' \
  -d '{"reason":"smoke"}'
case "$RESPONSE_STATUS" in
  200|202) say_ok "restart accepted $RESPONSE_STATUS"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "restart not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "restart $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 6) Health endpoint
skip_if_404 200 "GET /api/servers/{id}/health" -- "$(api_url /api/servers/smoke-srv/health)"

# 7) Activity endpoint (may be empty but must be 200)
skip_if_404 200 "GET /api/servers/{id}/activity" -- "$(api_url /api/servers/smoke-srv/activity)"

# 8) DELETE the server
capture_status "DELETE /api/servers/{id}" \
  -- -X DELETE "$(api_url /api/servers/smoke-srv)"
case "$RESPONSE_STATUS" in
  204|200) say_ok "delete server"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "delete not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "delete server $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 9) GET /api/admin/tenants (admin)
skip_if_404 200 "GET /api/admin/tenants" -- "$(api_url /api/admin/tenants)"

# 10) POST a tenant
TENANT_PAYLOAD='{
  "id":"smoke-tenant",
  "display_name":"Smoke Tenant",
  "plan":"free",
  "runtime_mode":"shared_global",
  "max_concurrent_sessions":4,
  "max_requests_per_minute":60,
  "audit_retention_days":7,
  "jwt_issuer":"https://issuer.example",
  "jwt_jwks_url":"https://issuer.example/.well-known/jwks.json"
}'
capture_status "POST /api/admin/tenants" \
  -- -X POST "$(api_url /api/admin/tenants)" \
  -H 'Content-Type: application/json' \
  -d "$TENANT_PAYLOAD"
case "$RESPONSE_STATUS" in
  201|200) say_ok "tenant created"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "tenant create not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "tenant create $RESPONSE_STATUS body=$RESPONSE_BODY"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 11) GET tenant back
skip_if_404 200 "GET /api/admin/tenants/{id}" -- "$(api_url /api/admin/tenants/smoke-tenant)"

# 12) Tenant activity
skip_if_404 200 "GET /api/admin/tenants/{id}/activity" -- "$(api_url /api/admin/tenants/smoke-tenant/activity)"

# 13) DELETE tenant (archive). Phase 10 wraps this verb with the
# approval gate so the first request returns 202 + approval_request_id
# (the operator re-issues with X-Approval-Token after approving). 202 is
# the gate's contract assertion; 204/200 still pass when the gate is
# bypassed (e.g. unconfigured approval store).
capture_status "DELETE /api/admin/tenants/{id}" \
  -- -X DELETE "$(api_url /api/admin/tenants/smoke-tenant)"
case "$RESPONSE_STATUS" in
  202) say_ok "archive tenant: approval gate engaged (202)"; PHASE_OK=$((PHASE_OK + 1));;
  204|200) say_ok "archive tenant"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "archive not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "archive $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 14) Secrets list (vault may be unconfigured in dev — accept 200/503)
capture_status "GET /api/admin/secrets" -- "$(api_url /api/admin/secrets)"
case "$RESPONSE_STATUS" in
  200|503) say_ok "secrets list ($RESPONSE_STATUS)"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "secrets list not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "secrets list $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 15) Secrets create (best-effort — vault must be configured)
SECRET_PAYLOAD='{"name":"smoke-secret","value":"hunter2","tenant_id":"preflight"}'
capture_status "POST /api/admin/secrets" \
  -- -X POST "$(api_url /api/admin/secrets)" \
  -H 'Content-Type: application/json' \
  -d "$SECRET_PAYLOAD"
case "$RESPONSE_STATUS" in
  201|200) say_ok "secret created"; PHASE_OK=$((PHASE_OK + 1));;
  503) say_ok "secret create handler wired (vault not configured)"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "secrets create not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "secret create $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 16) Reveal flow (skip if previous step skipped)
if [ "$RESPONSE_STATUS" = "200" ] || [ "$RESPONSE_STATUS" = "201" ]; then
  capture_status "POST /api/admin/secrets/{name}/reveal" \
    -- -X POST "$(api_url /api/admin/secrets/smoke-secret/reveal)"
  case "$RESPONSE_STATUS" in
    200)
      say_ok "reveal token issued"; PHASE_OK=$((PHASE_OK + 1))
      TOKEN=$(printf '%s' "$RESPONSE_BODY" | jq -r '.token' 2>/dev/null || echo "")
      if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
        capture_status "GET /api/admin/secrets/reveal/{token}" \
          -- "$(api_url /api/admin/secrets/reveal/$TOKEN)"
        if [ "$RESPONSE_STATUS" = "200" ]; then
          say_ok "reveal token consumed"; PHASE_OK=$((PHASE_OK + 1))
        else
          say_fail "reveal consume $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1))
        fi
      fi
      ;;
    503|404) say_skip "reveal not available"; PHASE_SKIP=$((PHASE_SKIP + 1));;
    *) say_fail "reveal issue $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
  esac
fi

# 17) Policy rules list
skip_if_404 200 "GET /api/policy/rules" -- "$(api_url /api/policy/rules)"

# 18) Policy rules POST
RULE_PAYLOAD='{
  "id":"smoke-rule",
  "priority":50,
  "enabled":true,
  "risk_class":"read",
  "conditions":{"match":{"tools":["github.list_repos"]}},
  "actions":{"allow":true},
  "notes":"smoke test"
}'
capture_status "POST /api/policy/rules" \
  -- -X POST "$(api_url /api/policy/rules)" \
  -H 'Content-Type: application/json' \
  -d "$RULE_PAYLOAD"
case "$RESPONSE_STATUS" in
  201|200) say_ok "policy rule created"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "policy not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "policy create $RESPONSE_STATUS body=$RESPONSE_BODY"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 19) Policy dry-run
DRY_PAYLOAD='{
  "call":{"server":"github","tool":"github.list_repos"}
}'
capture_status "POST /api/policy/dry-run" \
  -- -X POST "$(api_url /api/policy/dry-run)" \
  -H 'Content-Type: application/json' \
  -d "$DRY_PAYLOAD"
case "$RESPONSE_STATUS" in
  200) say_ok "dry-run evaluated"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "dry-run not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "dry-run $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

# 20) Policy rule delete
capture_status "DELETE /api/policy/rules/{id}" \
  -- -X DELETE "$(api_url /api/policy/rules/smoke-rule)"
case "$RESPONSE_STATUS" in
  204|200) say_ok "policy rule deleted"; PHASE_OK=$((PHASE_OK + 1));;
  404|405|501) say_skip "policy delete not wired"; PHASE_SKIP=$((PHASE_SKIP + 1));;
  *) say_fail "policy delete $RESPONSE_STATUS"; PHASE_FAIL=$((PHASE_FAIL + 1));;
esac

end_phase
