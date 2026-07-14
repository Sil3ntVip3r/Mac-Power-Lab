#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

SECONDS_TO_RUN="${1:-300}"
THREADS="${2:-}"

if [[ ! -x ./cpu_stress ]]; then
  ./build_tools.sh
fi

./set_phase.sh "CPU stress"

cleanup() {
  ./clear_phase.sh >/dev/null 2>&1 || true
}
trap cleanup INT TERM EXIT

if [[ -n "$THREADS" ]]; then
  ./cpu_stress "$SECONDS_TO_RUN" "$THREADS"
else
  ./cpu_stress "$SECONDS_TO_RUN"
fi
