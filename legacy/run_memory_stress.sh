#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

SECONDS_TO_RUN="${1:-300}"
MEMORY_MB="${2:-}"

if [[ ! -x ./memory_stress ]]; then
  ./build_tools.sh
fi

./set_phase.sh "memory bandwidth stress"

cleanup() {
  ./clear_phase.sh >/dev/null 2>&1 || true
}
trap cleanup INT TERM EXIT

if [[ -n "$MEMORY_MB" ]]; then
  ./memory_stress "$SECONDS_TO_RUN" "$MEMORY_MB"
else
  ./memory_stress "$SECONDS_TO_RUN"
fi
