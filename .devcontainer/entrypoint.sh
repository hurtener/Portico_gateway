#!/usr/bin/env bash
# Runtime bootstrap: inject secrets and configure git/gh before the build loop.
# Secrets arrive read-only at /run/secrets (bind mount) — never baked into the image.
set -uo pipefail
SEC=/run/secrets

echo "[entrypoint] bootstrapping $(date -u)"

# 1. opencode auth — the OAuth that authorizes opencode Zen + other providers.
if [ -f "$SEC/auth.json" ]; then
  mkdir -p /root/.local/share/opencode
  cp "$SEC/auth.json" /root/.local/share/opencode/auth.json
  chmod 600 /root/.local/share/opencode/auth.json
  echo "[entrypoint] opencode auth installed"
else
  echo "[entrypoint] WARN: no auth.json in /run/secrets — model calls may fail"
fi

# 2. NVIDIA NIM API key — authorizes the OpenAI-compatible NIM provider (primary model).
#    Exported for `exec "$@"` AND written to profile.d (the loop runs as `bash -lc`).
if [ -f "$SEC/nvidia_api_key" ]; then
  NVIDIA_API_KEY="$(tr -d '[:space:]' < "$SEC/nvidia_api_key")"
  export NVIDIA_API_KEY
  echo "export NVIDIA_API_KEY=$NVIDIA_API_KEY" > /etc/profile.d/nvidia.sh
  chmod 644 /etc/profile.d/nvidia.sh
  echo "[entrypoint] NVIDIA NIM key installed (NVIDIA_API_KEY set)"
else
  echo "[entrypoint] WARN: no nvidia_api_key in /run/secrets — NIM calls will fail"
fi

# 3. GitHub token — gh CLI (PRs) + git over HTTPS.
if [ -f "$SEC/gh_token" ]; then
  TOKEN="$(tr -d '[:space:]' < "$SEC/gh_token")"
  echo "$TOKEN" | gh auth login --with-token >/dev/null 2>&1 \
    && echo "[entrypoint] gh authenticated" \
    || echo "[entrypoint] WARN: gh auth login failed"
  git config --global url."https://x-access-token:${TOKEN}@github.com/".insteadOf "https://github.com/"
else
  echo "[entrypoint] WARN: no gh_token — pushes and PRs will fail"
fi

# 4. git identity — match the host's PERSONAL account (never the work one).
git config --global user.name "hurtener"
git config --global user.email "benvenuto.santiago@hotmail.com"
git config --global commit.gpgsign false
git config --global tag.gpgsign false
git config --global init.defaultBranch main
git config --global --add safe.directory /workspace/portico

# 5. Warm the Go build cache once so the first loop iteration's gates aren't slow.
#    Best-effort; never fail the bootstrap on a transient network hiccup.
( cd /workspace/portico && go mod download 2>/dev/null ) || true

echo "[entrypoint] ready"
exec "$@"
