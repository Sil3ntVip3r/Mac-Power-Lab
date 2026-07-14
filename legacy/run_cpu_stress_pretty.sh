#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-300}"
STAMP="$(date +%Y%m%d_%H%M%S)"

chmod +x friendly_test_ui.py
./friendly_test_ui.py   --title "CPU stress test"   --duration "$DURATION"   --phase "CPU stress test"   --log "logs/friendly_cpu_${STAMP}.log"   --meta "test_type=cpu"   --meta "duration=${DURATION}"   -- ./run_cpu_stress.sh "$DURATION"
