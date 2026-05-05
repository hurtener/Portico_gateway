#!/usr/bin/env bash
# Common helpers shared by all phase smoke scripts.
# Source this from each phase-N.sh before running checks.
#
# Each phase script must:
#   - Source this file.
#   - Call begin_phase "<phase name>".
#   - Run checks via assert_status / assert_json / skip_if_404 / etc.
#   - Call end_phase to write counters and return appropriate exit code.

set -euo pipefail

# Colors (only when on a TTY)
if [ -t 1 ]; then
  C_RED='\033[0;31m'
  C_GREEN='\033[0;32m'
  C_YELLOW='\033[0;33m'
  C_BLUE='\033[0;34m'
  C_RESET='\033[0m'
else
  C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_RESET=''
fi

# --- Output helpers -------------------------------------------------------

say_step() { printf "${C_BLUE}==>${C_RESET} %s\n" "$*"; }
say_ok()   { printf "${C_GREEN}OK${C_RESET}   %s\n" "$*"; }
say_skip() { printf "${C_YELLOW}SKIP${C_RESET} %s\n" "$*"; }
say_fail() { printf "${C_RED}FAIL${C_RESET} %s\n" "$*"; }
say_warn() { printf "${C_YELLOW}WARN${C_RESET} %s\n" "$*" 1>&2; }
say_err()  { printf "${C_RED}ERR${C_RESET}  %s\n"  "$*" 1>&2; }

# --- Tooling --------------------------------------------------------------

require_cmd() {
  local cmd=$1
  if ! command -v "$cmd" >/dev/null 2>&1; then
    say_err "required command '$cmd' not found in PATH"
    exit 1
  fi
}

# Wait for /healthz on the given port, up to N seconds.
wait_for_health() {
  local port=$1
  local timeout=${2:-30}
  local start
  start=$(date +%s)
  while ! curl -sf "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; do
    if [ "$(( $(date +%s) - start ))" -ge "$timeout" ]; then
      return 1
    fi
    sleep 0.2
  done
  return 0
}

# --- Phase counters -------------------------------------------------------

# Per-phase script API.
# These are local to the script's process; preflight.sh harvests them via
# .preflight-counters when each phase completes.

begin_phase() {
  PHASE_NAME="${1:-unnamed}"
  PHASE_OK=0
  PHASE_SKIP=0
  PHASE_FAIL=0
  say_step "phase: $PHASE_NAME"
}

end_phase() {
  local rc=0
  if [ "$PHASE_FAIL" -gt 0 ]; then rc=1; fi
  printf "PHASE_OK=%s\nPHASE_SKIP=%s\nPHASE_FAIL=%s\n" "$PHASE_OK" "$PHASE_SKIP" "$PHASE_FAIL" \
    > "$(git rev-parse --show-toplevel 2>/dev/null || pwd)/.preflight-counters"
  printf "  -> %d ok, %d skip, %d fail\n" "$PHASE_OK" "$PHASE_SKIP" "$PHASE_FAIL"
  return $rc
}

# --- Curl + assertion helpers --------------------------------------------

# Usage: capture_status <description> -- <curl args...>
# Sets RESPONSE_BODY to the body and RESPONSE_STATUS to the HTTP code.
# Always returns 0; assertion is the caller's job.
capture_status() {
  local description=$1; shift
  # Drop the literal "--" separator if present
  if [ "${1:-}" = "--" ]; then shift; fi
  local body_file
  body_file=$(mktemp -t portico-resp.XXXXXX)
  RESPONSE_STATUS=$(curl -s -o "$body_file" -w "%{http_code}" "$@" || echo "000")
  RESPONSE_BODY=$(cat "$body_file")
  rm -f "$body_file"
  RESPONSE_DESCRIPTION="$description"
}

# assert_status <expected_status> <description> -- <curl args>
assert_status() {
  local expected=$1; local description=$2; shift 2
  capture_status "$description" "$@"
  if [ "$RESPONSE_STATUS" = "$expected" ]; then
    say_ok "$description (HTTP $RESPONSE_STATUS)"
    PHASE_OK=$((PHASE_OK + 1))
    return 0
  fi
  say_fail "$description (expected HTTP $expected, got $RESPONSE_STATUS)"
  if [ -n "${RESPONSE_BODY:-}" ]; then
    echo "       body: $RESPONSE_BODY" | head -c 500
    echo
  fi
  PHASE_FAIL=$((PHASE_FAIL + 1))
  return 1
}

# skip_if_404 <expected_status> <description> -- <curl args>
# Same as assert_status but treats 404 as SKIP (feature not yet implemented).
skip_if_404() {
  local expected=$1; local description=$2; shift 2
  capture_status "$description" "$@"
  case "$RESPONSE_STATUS" in
    "$expected")
      say_ok "$description (HTTP $RESPONSE_STATUS)"
      PHASE_OK=$((PHASE_OK + 1))
      return 0
      ;;
    404|405|501)
      say_skip "$description (HTTP $RESPONSE_STATUS — feature not implemented in this build)"
      PHASE_SKIP=$((PHASE_SKIP + 1))
      return 0
      ;;
    *)
      say_fail "$description (expected HTTP $expected, got $RESPONSE_STATUS)"
      if [ -n "${RESPONSE_BODY:-}" ]; then
        echo "       body: $RESPONSE_BODY" | head -c 500
        echo
      fi
      PHASE_FAIL=$((PHASE_FAIL + 1))
      return 1
      ;;
  esac
}

# assert_json_path <jq-path-expr> <expected> <description>
# Operates on the most recent RESPONSE_BODY.
assert_json_path() {
  local path=$1; local expected=$2; local description=$3
  local actual
  actual=$(printf '%s' "$RESPONSE_BODY" | jq -r "$path" 2>/dev/null || echo "<jq-error>")
  if [ "$actual" = "$expected" ]; then
    say_ok "$description ($path = $expected)"
    PHASE_OK=$((PHASE_OK + 1))
    return 0
  fi
  say_fail "$description (expected $path = '$expected', got '$actual')"
  PHASE_FAIL=$((PHASE_FAIL + 1))
  return 1
}

# assert_json_truthy <jq-path-expr> <description>
# Passes if the path resolves to a non-empty, non-null value.
assert_json_truthy() {
  local path=$1; local description=$2
  local actual
  actual=$(printf '%s' "$RESPONSE_BODY" | jq -r "$path" 2>/dev/null || echo "")
  if [ -n "$actual" ] && [ "$actual" != "null" ]; then
    say_ok "$description ($path is truthy)"
    PHASE_OK=$((PHASE_OK + 1))
    return 0
  fi
  say_fail "$description ($path is empty/null)"
  PHASE_FAIL=$((PHASE_FAIL + 1))
  return 1
}

# Build common URLs from the orchestrator-provided base.
api_url() {
  local path=$1
  printf '%s%s' "${PORTICO_PREFLIGHT_BASE_URL:-http://127.0.0.1:18080}" "$path"
}
mcp_url() {
  api_url "/mcp"
}

# JSON-RPC helper.
# Usage: jsonrpc <method> '<params-json>' [request-id]
jsonrpc() {
  local method=$1
  local params=${2:-'{}'}
  local id=${3:-1}
  printf '{"jsonrpc":"2.0","id":%s,"method":"%s","params":%s}' "$id" "$method" "$params"
}
