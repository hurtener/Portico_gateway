#!/usr/bin/env bash
# Phase 10.9 smoke: gateway info endpoint.
#
# /api/gateway/info is a public read-only endpoint that surfaces the
# bind address, MCP path, and auth requirements so the Console (and
# external operators) can assemble a correct client config without
# reading portico.yaml. The endpoint exposes nothing that isn't
# already observable from a TCP probe + JWKS fetch.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-10.9 (operability + connect)"

# 1) Endpoint exists and returns 200.
skip_if_404 200 "GET /api/gateway/info" \
  -- "$(api_url /api/gateway/info)"

if [ "$RESPONSE_STATUS" = "200" ]; then
  # 2) Required top-level fields are populated.
  assert_json_truthy ".bind"      "bind address present"
  assert_json_truthy ".mcp_path"  "MCP path present"
  assert_json_truthy ".auth.mode" "auth mode present"

  # 3) MCP path is the canonical /mcp.
  assert_json_path ".mcp_path" "/mcp" "mcp_path is /mcp"
fi

end_phase
