#!/usr/bin/env bash
# Phase 11 smoke: telemetry replay & time-travel inspector surface.
#
# Verifies the Phase 11 endpoints are mounted, return the right shapes
# for empty state, and refuse malformed inputs cleanly. The inspector
# requires a real session row (Phase 6 snapshot binder writes it on
# the first MCP tool call) so the bundle/export paths run against a
# missing session id and assert the typed 404 — that's the right
# signal that the route is wired and tenant-scoped.

set -euo pipefail
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

begin_phase "phase-11 (telemetry replay)"

# 1) /api/audit/search returns 200 + empty events on a fresh tenant.
skip_if_404 200 "GET /api/audit/search (empty tenant)" \
  -- "$(api_url '/api/audit/search?limit=5')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.events | type' 'array' "audit search returns events array"
fi

# 2) /api/spans rejects missing filter with a typed 400.
skip_if_404 400 "GET /api/spans without filter" \
  -- "$(api_url '/api/spans')"
if [ "$RESPONSE_STATUS" = "400" ]; then
  assert_json_path '.error' 'missing_filter' "spans without filter returns missing_filter"
fi

# 3) /api/spans?session_id=… returns empty array (no spans seeded yet).
skip_if_404 200 "GET /api/spans?session_id=does-not-exist" \
  -- "$(api_url '/api/spans?session_id=phase-11-smoke-missing')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.spans | type' 'array' "spans by session returns array"
fi

# 4) /api/sessions/imported returns 200 + empty list.
skip_if_404 200 "GET /api/sessions/imported (empty)" \
  -- "$(api_url '/api/sessions/imported')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.imported | type' 'array' "imported list returns array"
fi

# 5) /api/sessions/{sid}/bundle returns 404 for a missing session.
skip_if_404 404 "GET /api/sessions/missing/bundle" \
  -- "$(api_url '/api/sessions/phase-11-missing/bundle')"
if [ "$RESPONSE_STATUS" = "404" ]; then
  assert_json_path '.error' 'session_not_found' "bundle for missing session returns session_not_found"
fi

# 6) /api/sessions/{sid}/export returns 404 for missing session.
skip_if_404 404 "POST /api/sessions/missing/export" \
  -- -X POST "$(api_url '/api/sessions/phase-11-missing/export')"

# 7) /api/sessions/import refuses garbage with bundle_corrupt.
JUNK=$(mktemp -t portico-junk.XXXXXX)
echo "not a real bundle" > "$JUNK"
skip_if_404 400 "POST /api/sessions/import (junk body)" \
  -- -X POST "$(api_url '/api/sessions/import')" \
       -H "Content-Type: application/gzip" \
       --data-binary "@$JUNK"
if [ "$RESPONSE_STATUS" = "400" ]; then
  assert_json_path '.error' 'bundle_corrupt' "junk import returns bundle_corrupt"
fi
rm -f "$JUNK"

# 8) /api/audit/search with a query string runs the FTS index.
skip_if_404 200 "GET /api/audit/search?q=phase-11" \
  -- "$(api_url '/api/audit/search?q=phase-11&limit=5')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.events | type' 'array' "audit search with query returns array"
fi

# 9) /api/spans?trace_id=… returns 200 even when no traces match.
skip_if_404 200 "GET /api/spans?trace_id=missing" \
  -- "$(api_url '/api/spans?trace_id=phase-11-smoke-trace')"
if [ "$RESPONSE_STATUS" = "200" ]; then
  assert_json_path '.spans | type' 'array' "spans by trace returns array"
fi

# 10) Replay endpoint refuses imported sessions with 409.
skip_if_404 409 "POST replay on imported session" \
  -- -X POST "$(api_url '/api/sessions/imported:phase-11-smoke/calls/cid-1/replay')" \
       -H "Content-Type: application/json" \
       --data '{"kind":"tool_call","target":"github.search","payload":{}}'
if [ "$RESPONSE_STATUS" = "409" ]; then
  assert_json_path '.error' 'replay_imported_disallowed' "imported replay returns typed error"
fi

# 11) Replay endpoint rejects missing kind/target with 400.
skip_if_404 400 "POST replay with empty body" \
  -- -X POST "$(api_url '/api/sessions/phase-11-smoke/calls/cid-1/replay')" \
       -H "Content-Type: application/json" \
       --data '{}'
if [ "$RESPONSE_STATUS" = "400" ]; then
  assert_json_path '.error' 'missing_field' "replay missing-field returns typed error"
fi

end_phase
