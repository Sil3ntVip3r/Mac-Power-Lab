#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-300}"
STAMP="$(date +%Y%m%d_%H%M%S)"

chmod +x friendly_test_ui.py
./friendly_test_ui.py   --title "Memory bandwidth stress test"   --duration "$DURATION"   --phase "Memory bandwidth stress test"   --log "logs/friendly_memory_${STAMP}.log"   --meta "test_type=memory"   --meta "duration=${DURATION}"   -- ./run_memory_stress.sh "$DURATION"
