#!/usr/bin/env bash
# Install Portico's git hooks. Idempotent.
#
# Configures git to use .githooks/ as the hooks dir, so hook updates can be
# tracked in the repo (unlike the default .git/hooks which is per-clone and
# untracked).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [ ! -d .githooks ]; then
  echo "ERR: .githooks/ not found in repo root"
  exit 1
fi

# Make sure all hooks are executable
chmod +x .githooks/* 2>/dev/null || true

git config core.hooksPath .githooks
echo "git hooks configured: core.hooksPath=.githooks"

# Sanity: list active hooks
echo "active hooks:"
ls -1 .githooks/ | sed 's/^/  /'

cat <<'EOF'

Hooks installed. They run automatically on the matching git operation.

To bypass in an emergency:
  PORTICO_PREFLIGHT_SKIP=1 git commit ...
  git commit --no-verify   (last resort; document why in the commit message)

CI runs the same checks regardless of local hook state.
EOF
