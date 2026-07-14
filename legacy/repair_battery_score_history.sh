#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x repair_battery_score_history.py
./repair_battery_score_history.py "$@"
