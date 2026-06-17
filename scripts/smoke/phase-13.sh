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

# 3) GET /v1/models mounted: returns 200 + OpenAI shape.
skip_if_404 200 "GET /v1/models (list)" -- "$(api_url '/v1/models')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.object' 'list' "models list returns object=list"
  assert_json_path '.data | type' 'array' "models list returns data array"
fi

# 4) POST /v1/embeddings mounted: empty body -> typed 400 (model+input required).
skip_if_404 400 "POST /v1/embeddings (empty body)" \
  -- -X POST "$(api_url '/v1/embeddings')" \
     -H "Content-Type: application/json" --data '{}'
if [ "$RESPONSE_STATUS" = "400" ]; then
  assert_json_path '.error' 'invalid_request' "embeddings rejects empty request with typed error"
fi

# 5) Unknown model alias for embeddings -> typed 404 model_not_found.
#    Only asserted when check 4 proved the route is mounted.
if [ "$RESPONSE_STATUS" = "400" ]; then
  capture_status "POST /v1/embeddings (unknown model)" \
    -X POST "$(api_url '/v1/embeddings')" \
    -H "Content-Type: application/json" \
    --data '{"model":"no-such-alias","input":["hi"]}'
  if [ "$RESPONSE_STATUS" = "404" ]; then
    assert_json_path '.error' 'model_not_found' "unknown model alias for embeddings returns typed 404"
  else
    say_fail "expected 404 model_not_found for embeddings, got $RESPONSE_STATUS"
    PHASE_FAIL=$((PHASE_FAIL + 1))
  fi
fi

# 5b) Streaming chat: a stream:true request now reaches alias resolution (no more
#     stream_unsupported); with an unknown model it returns the typed 404.
capture_status "POST /v1/chat/completions stream:true (unknown model)" \
  -X POST "$(api_url '/v1/chat/completions')" \
  -H "Content-Type: application/json" \
  --data '{"model":"no-such-alias","stream":true,"messages":[{"role":"user","content":"hi"}]}'
if [ "$RESPONSE_STATUS" = "404" ]; then
  assert_json_path '.error' 'model_not_found' "stream request reaches alias resolution (not stream_unsupported)"
elif [ "$RESPONSE_STATUS" = "501" ] || [ "$RESPONSE_STATUS" = "405" ]; then
  say_skip "streaming chat not mounted (HTTP $RESPONSE_STATUS)"
else
  say_fail "stream request: expected 404 model_not_found, got $RESPONSE_STATUS"
  PHASE_FAIL=$((PHASE_FAIL + 1))
fi

# 6) Provider CRUD round-trip (dev-mode identity is admin).
skip_if_404 200 "GET /api/llm/providers (list)" -- "$(api_url '/api/llm/providers')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.providers | type' 'array' "providers list returns an array"

  assert_status 201 "POST /api/llm/providers (create)" \
    -- -X POST "$(api_url '/api/llm/providers')" -H "Content-Type: application/json" \
       --data '{"name":"smoke-prov","driver":"openai","config":{"base_url":"https://example.invalid"},"enabled":true}'

  assert_status 200 "GET /api/llm/providers/smoke-prov" -- "$(api_url '/api/llm/providers/smoke-prov')"
  assert_json_path '.driver' 'openai' "created provider round-trips"

  # 7) Model alias CRUD (references the provider just created).
  assert_status 201 "POST /api/llm/models (create)" \
    -- -X POST "$(api_url '/api/llm/models')" -H "Content-Type: application/json" \
       --data '{"alias":"smoke-model","provider_name":"smoke-prov","provider_model":"gpt-4o","enabled":true}'
  assert_status 200 "GET /api/llm/models/smoke-model" -- "$(api_url '/api/llm/models/smoke-model')"
  assert_json_path '.provider_model' 'gpt-4o' "created model round-trips"
fi

# 8) Per-tenant LLM quota GET/PUT (one row per tenant; dev identity is admin).
skip_if_404 200 "GET /api/llm/quota" -- "$(api_url '/api/llm/quota')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.requests_per_minute | type' 'number' "quota exposes requests_per_minute"

  assert_status 200 "PUT /api/llm/quota (upsert)" \
    -- -X PUT "$(api_url '/api/llm/quota')" -H "Content-Type: application/json" \
       --data '{"requests_per_minute":120,"tokens_per_minute":50000,"tokens_per_day":1000000,"cost_usd_per_day":25}'
  assert_json_path '.requests_per_minute' '120' "quota update round-trips"
fi

# 9) Cost telemetry: daily rollups + global price book (dev identity is admin).
skip_if_404 200 "GET /api/llm/costs" -- "$(api_url '/api/llm/costs')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.summary | type' 'object' "costs response carries a summary"
  assert_json_path '.daily | type' 'array' "costs response carries a daily array"

  assert_status 200 "PUT /api/llm/costs/prices (upsert)" \
    -- -X PUT "$(api_url '/api/llm/costs/prices')" -H "Content-Type: application/json" \
       --data '{"provider_driver":"openai","provider_model":"gpt-4o","input_per_1k":2.5,"output_per_1k":10}'
  skip_if_404 200 "GET /api/llm/costs/prices" -- "$(api_url '/api/llm/costs/prices')"
  assert_json_path '.prices | type' 'array' "price book returns an array"
fi

end_phase
