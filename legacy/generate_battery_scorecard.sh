#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x generate_battery_scorecard.py
./generate_battery_scorecard.py "$@"
