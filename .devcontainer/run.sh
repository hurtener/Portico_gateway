#!/usr/bin/env bash
# Host-side orchestration: prepare secrets, build the image, and launch the
# plan-driven autonomous build loop detached inside the container. Run from the host.
#
#   ./.devcontainer/run.sh                 # default: portico-build command, NIM primary
#   MAX_ITERS=50 ./.devcontainer/run.sh    # cap iterations for a short slice
#   MODEL=... ./.devcontainer/run.sh       # override the primary model
#
# Switch targets by editing .devcontainer/TASK.md, then re-running this script
# (it recreates the container — NEVER `docker start` a stopped one; that resurrects
# the old loop with stale task env and causes dual-loop API contention).
set -euo pipefail

REPO="/Volumes/m2-extended-disk/Repos/Portico_gateway"
DC="$REPO/.devcontainer"
IMAGE="portico-builder"
NAME="portico-builder"
COMMAND="${1:-portico-build}"
# PRIMARY model = NVIDIA NIM Nemotron-3 Ultra 550B (free, no usage cap). The
# GLM-5.2 free window (via the HF Router) has ended; the HF provider block stays
# in opencode.json so MODEL=huggingface/zai-org/GLM-5.2:fireworks-ai can re-select
# it if the offer returns. The loop backs off / retries on a rate-limit signal.
MODEL="${MODEL:-nvidia/nvidia/nemotron-3-ultra-550b-a55b}"
VARIANT="${VARIANT-}"

echo "[run] preparing secrets..."
mkdir -p "$DC/secrets" "$DC/var"
gh auth token > "$DC/secrets/gh_token"
cp ~/.local/share/opencode/auth.json "$DC/secrets/auth.json"
chmod 600 "$DC/secrets/gh_token" "$DC/secrets/auth.json"
# NIM key: env override wins; otherwise keep the existing gitignored secret file. Never hardcode it.
if [ -n "${NVIDIA_API_KEY:-}" ]; then printf '%s' "$NVIDIA_API_KEY" > "$DC/secrets/nvidia_api_key"; fi
if [ -f "$DC/secrets/nvidia_api_key" ]; then chmod 600 "$DC/secrets/nvidia_api_key"; else echo "[run] WARN: no secrets/nvidia_api_key — NIM calls will fail"; fi
# HF token: env override wins; otherwise keep the existing gitignored secret file. Never hardcode it.
if [ -n "${HF_TOKEN:-}" ]; then printf '%s' "$HF_TOKEN" > "$DC/secrets/hf_token"; fi
if [ -f "$DC/secrets/hf_token" ]; then chmod 600 "$DC/secrets/hf_token"; else echo "[run] WARN: no secrets/hf_token — HF/GLM calls will fail"; fi
chmod +x "$DC/loop.sh" "$DC/entrypoint.sh"

echo "[run] building image (first build pulls Go 1.25 + Node 22 + opencode + tooling)..."
docker build -t "$IMAGE" "$DC"

echo "[run] (re)starting container (recreate, never docker-start — avoids dual-loop contention)..."
docker rm -f "$NAME" >/dev/null 2>&1 || true
# The loop is PID 1. --restart on-failure brings it back on a crash (OOM, daemon hiccup),
# but a clean [goal:complete] exits 0 so it stays stopped. Port 8080 (portico dev) is
# published to host loopback for orchestrator-side Playwright validation of the live Console.
docker run -d --name "$NAME" \
  --restart on-failure:20 \
  -p 127.0.0.1:8080:8080 \
  -v "$REPO:/workspace/portico" \
  -v "$DC/secrets:/run/secrets:ro" \
  -v "$DC/var:/var/portico" \
  -e COMMAND="$COMMAND" \
  -e MODEL="$MODEL" \
  -e VARIANT="$VARIANT" \
  -e MAX_ITERS="${MAX_ITERS:-2000}" \
  "$IMAGE" \
  bash -lc '/workspace/portico/.devcontainer/loop.sh'

echo "[run] container up (command=$COMMAND, model=$MODEL). Bootstrap runs before the loop."
echo "[run] monitor:  tail -f $DC/var/run.log   |   cat $DC/var/status.txt"
echo "[run] keep the host awake for long runs:  caffeinate -dimsu -w \$(pgrep -f run.log) &"
