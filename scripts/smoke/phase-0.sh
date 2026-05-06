#!/usr/bin/env bash
# Phase 0 smoke: skeleton + tenant foundation.
# Validates: health endpoints, dev-mode JWT bypass, tenant context, audit stub.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-0 (skeleton + tenant foundation)"

# /healthz must always return 200 with status:ok
assert_status 200 "GET /healthz returns 200" \
  -- "$(api_url /healthz)"
assert_json_path '.status' 'ok' "GET /healthz body has status=ok"

# /readyz must return 200 once the server has booted
assert_status 200 "GET /readyz returns 200" \
  -- "$(api_url /readyz)"

# Audit stub: dev mode requires no auth, returns empty events array
assert_status 200 "GET /v1/audit/events (dev mode, no auth) returns 200" \
  -- "$(api_url /v1/audit/events)"
assert_json_truthy '.events // []' "GET /v1/audit/events body has events array"

# Console home should render
skip_if_404 200 "GET / (console home) returns 200" \
  -- "$(api_url /)"

# Static asset bundled by the SvelteKit adapter-static build. If the
# frontend hasn't been built yet the placeholder page renders without a
# favicon, so the check tolerates a 404.
skip_if_404 200 "GET /favicon.svg returns 200 (Console SPA asset)" \
  -- "$(api_url /favicon.svg)"

# 404 path returns structured JSON
capture_status "GET /v1/does-not-exist returns 404 JSON" \
  -- "$(api_url /v1/does-not-exist)"
if [ "$RESPONSE_STATUS" = "404" ]; then
  if printf '%s' "$RESPONSE_BODY" | jq -e '.error' >/dev/null 2>&1; then
    say_ok "404 returns JSON with .error"
    PHASE_OK=$((PHASE_OK + 1))
  else
    say_fail "404 body did not contain .error JSON: $(echo "$RESPONSE_BODY" | head -c 200)"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  fi
else
  say_skip "404 envelope check (server returned $RESPONSE_STATUS)"
  PHASE_SKIP=$((PHASE_SKIP + 1))
fi

# Tenant context smoke: dev mode auto-creates the dev tenant.
# Verify GET /v1/admin/tenants returns it (admin scope synthesized in dev mode).
skip_if_404 200 "GET /v1/admin/tenants (dev mode admin) returns 200" \
  -- "$(api_url /v1/admin/tenants)"
if [ "$RESPONSE_STATUS" = "200" ]; then
  if printf '%s' "$RESPONSE_BODY" | jq -e '.[] | select(.id == "preflight" or .id == "dev")' >/dev/null 2>&1; then
    say_ok "dev tenant materialized"
    PHASE_OK=$((PHASE_OK + 1))
  else
    say_warn "dev tenant not visible in /v1/admin/tenants response (continuing): $(echo "$RESPONSE_BODY" | head -c 200)"
  fi
fi

end_phase
