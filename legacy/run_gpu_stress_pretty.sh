#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-300}"
PROFILE="${2:-high}"
STAMP="$(date +%Y%m%d_%H%M%S)"

chmod +x friendly_test_ui.py
./friendly_test_ui.py   --title "GPU stress test (${PROFILE})"   --duration "$DURATION"   --phase "GPU stress test (${PROFILE})"   --log "logs/friendly_gpu_${PROFILE}_${STAMP}.log"   --meta "test_type=gpu"   --meta "gpu_profile=${PROFILE}"   --meta "duration=${DURATION}"   -- ./run_gpu_stress.sh "$DURATION" "$PROFILE"
