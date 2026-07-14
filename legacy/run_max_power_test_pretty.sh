#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-300}"
GPU_PROFILE="${2:-high}"
MEMORY_MB="${3:-auto}"
STAMP="$(date +%Y%m%d_%H%M%S)"

chmod +x friendly_test_ui.py
./friendly_test_ui.py   --title "MAX POWER: CPU + GPU + memory"   --duration "$DURATION"   --phase "MAX POWER: CPU + GPU + memory"   --log "logs/friendly_max_power_${STAMP}.log"   --meta "test_type=max_power"   --meta "gpu_profile=${GPU_PROFILE}"   --meta "memory_mb=${MEMORY_MB}"   --meta "duration=${DURATION}"   -- ./run_max_power_test.sh "$DURATION" "$GPU_PROFILE" "$MEMORY_MB"
