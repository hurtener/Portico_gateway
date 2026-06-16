#!/usr/bin/env bash
# Outer autonomous build loop. Each iteration is a FRESH stateless opencode session
# that re-orients from git state + the orchestrator's target brief (.devcontainer/TASK.md),
# so context stays lean across a long build (the git history IS the durable progress).
# Primary model: NVIDIA NIM Nemotron-3 Ultra 550B.
set -uo pipefail

# Login shells reset PATH; ensure all toolchain bins are reachable.
export PATH="/usr/local/bun/bin:/root/.opencode/bin:/usr/local/go/bin:/go/bin:/usr/local/bin:$PATH"

cd /workspace/portico

# Primary + fallback default to the SAME NIM model: NIM has generous limits, so a
# rate-limit just becomes a back-off rather than a quality downgrade. Override
# FALLBACK_MODEL to route elsewhere on a sustained limit.
PRIMARY_MODEL="${MODEL:-nvidia/nvidia/nemotron-3-ultra-550b-a55b}"
PRIMARY_VARIANT="${VARIANT-}"   # empty VARIANT omits --variant
FALLBACK_MODEL="${FALLBACK_MODEL:-nvidia/nvidia/nemotron-3-ultra-550b-a55b}"
FALLBACK_VARIANT="${FALLBACK_VARIANT-}"
ACTIVE="primary"
FB_ITERS=0
RETRY_PRIMARY_EVERY="${RETRY_PRIMARY_EVERY:-4}"
CMD_NAME="${COMMAND:-portico-build}"
MAX_ITERS="${MAX_ITERS:-2000}"
LOG="${LOG:-/var/portico/run.log}"
STATUS="/var/portico/status.txt"

mkdir -p "$(dirname "$LOG")"
note() { echo "$(date -u +%H:%M:%S) | $*" | tee -a "$LOG" >&2; }

note "=== portico build loop start (primary=$PRIMARY_MODEL fallback=$FALLBACK_MODEL command=$CMD_NAME max=$MAX_ITERS) ==="
echo "RUNNING" > "$STATUS"

i=0
while [ "$i" -lt "$MAX_ITERS" ]; do
  i=$((i + 1))

  # Periodically retry the primary after running on the fallback for a while.
  if [ "$ACTIVE" = "fallback" ] && [ "$FB_ITERS" -ge "$RETRY_PRIMARY_EVERY" ]; then
    note "retrying primary ($PRIMARY_MODEL) after $FB_ITERS fallback iterations"
    ACTIVE="primary"; FB_ITERS=0
  fi
  if [ "$ACTIVE" = "primary" ]; then
    CUR_MODEL="$PRIMARY_MODEL"; CUR_VARIANT="$PRIMARY_VARIANT"
  else
    CUR_MODEL="$FALLBACK_MODEL"; CUR_VARIANT="$FALLBACK_VARIANT"; FB_ITERS=$((FB_ITERS + 1))
  fi

  note "--- iteration $i (model=$CUR_MODEL${CUR_VARIANT:+ variant=$CUR_VARIANT}) ---"

  variant_args=()
  [ -n "$CUR_VARIANT" ] && variant_args=(--variant "$CUR_VARIANT")
  out="$(opencode run --model "$CUR_MODEL" "${variant_args[@]}" --command "$CMD_NAME" 2>&1)"
  printf '%s\n' "$out" >> "$LOG"

  if printf '%s' "$out" | grep -q '\[goal:complete\]'; then
    note "COMPLETE — unit reported done at iteration $i"
    echo "COMPLETE" > "$STATUS"
    break
  fi
  if printf '%s' "$out" | grep -q '\[goal:blocked\]'; then
    note "BLOCKED — model reported a blocker at iteration $i"
    echo "BLOCKED" > "$STATUS"
    break
  fi
  if printf '%s' "$out" | grep -qiE 'rate.?limit|quota exceeded|usage limit|429|402|insufficient[_ ]?(quota|credit|fund)'; then
    if [ "$ACTIVE" = "primary" ]; then
      note "LIMIT on primary $CUR_MODEL (iter $i) → switching to fallback $FALLBACK_MODEL"
      echo "RATE_LIMITED->fallback (iter $i)" > "$STATUS"
      ACTIVE="fallback"; FB_ITERS=0
    else
      note "LIMIT on fallback $CUR_MODEL (iter $i) — backing off 120s"
      echo "RATE_LIMITED (iter $i)" > "$STATUS"
      sleep 120
    fi
  fi

  sleep 5
done

note "=== loop ended (iterations=$i) ==="
if [ -f "$STATUS" ] && [ "$(cat "$STATUS")" = "RUNNING" ]; then
  echo "MAX_ITERS_REACHED" > "$STATUS"
fi
# Always exit 0 on a clean end so the container's restart policy does NOT relaunch
# the loop after [goal:complete] (a non-zero exit would cause a restart loop).
exit 0
