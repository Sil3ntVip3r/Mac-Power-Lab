#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

SECONDS_TO_RUN="${1:-300}"
GPU_PROFILE="${2:-high}"
BUFFER_MB="${3:-}"

if [[ ! -x ./gpu_stress ]]; then
  ./build_tools.sh
fi

./set_phase.sh "GPU stress: $GPU_PROFILE"

cleanup() {
  ./clear_phase.sh >/dev/null 2>&1 || true
}
trap cleanup INT TERM EXIT

if [[ -n "$BUFFER_MB" ]]; then
  ./gpu_stress "$SECONDS_TO_RUN" "$GPU_PROFILE" "$BUFFER_MB"
else
  ./gpu_stress "$SECONDS_TO_RUN" "$GPU_PROFILE"
fi
