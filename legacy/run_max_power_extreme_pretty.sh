#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-300}"
MEMORY_MB="${2:-auto}"
STAMP="$(date +%Y%m%d_%H%M%S)"

chmod +x friendly_test_ui.py
./friendly_test_ui.py   --title "EXTREME POWER: CPU + extreme GPU + memory"   --duration "$DURATION"   --phase "EXTREME POWER: CPU + extreme GPU + memory"   --log "logs/friendly_extreme_power_${STAMP}.log"   --meta "test_type=extreme_power"   --meta "gpu_profile=extreme"   --meta "memory_mb=${MEMORY_MB}"   --meta "duration=${DURATION}"   -- ./run_max_power_extreme.sh "$DURATION" "$MEMORY_MB"
