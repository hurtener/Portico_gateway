#!/usr/bin/env bash
# Phase 13 smoke: LLM gateway (OpenAI-compatible northbound).
#
# Verifies POST /v1/chat/completions is mounted and the handler runs end to end
# (tenant + scope + request validation) without needing a configured provider:
# an empty request body returns a typed 400. When the route is not yet built the
# server 404s and the check SKIPs, per the phase-gate convention.
set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-13 (llm gateway)"

# 1) /v1/chat/completions mounted: empty body -> typed 400 (model+messages required).
skip_if_404 400 "POST /v1/chat/completions (empty body)" \
  -- -X POST "$(api_url '/v1/chat/completions')" \
     -H "Content-Type: application/json" --data '{}'
if [ "$RESPONSE_STATUS" = "400" ]; then
  assert_json_path '.error' 'invalid_request' "chat completions rejects empty request with typed error"
fi

# 2) Unknown model alias -> typed 404 model_not_found (alias-resolution path runs).
#    Only asserted when check 1 proved the route is mounted (avoids a bare-404 false pass).
if [ "$RESPONSE_STATUS" = "400" ]; then
  capture_status "POST /v1/chat/completions (unknown model)" \
    -X POST "$(api_url '/v1/chat/completions')" \
    -H "Content-Type: application/json" \
    --data '{"model":"no-such-alias","messages":[{"role":"user","content":"hi"}]}'
  if [ "$RESPONSE_STATUS" = "404" ]; then
    assert_json_path '.error' 'model_not_found' "unknown model alias returns typed 404"
  else
    say_fail "expected 404 model_not_found, got $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  fi
fi

end_phase
