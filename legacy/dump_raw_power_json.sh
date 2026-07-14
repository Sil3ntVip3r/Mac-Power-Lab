#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p logs
OUT="logs/raw_power_$(date +%Y%m%d_%H%M%S).json"
./mac_power_watch.py --dump-raw-json > "$OUT"
echo "Raw power JSON written to:"
echo "$OUT"
