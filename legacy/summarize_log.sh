#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

if [[ "${1:-}" == "" ]]; then
  CSV_FILE="$(ls -t logs/mac_power_*.csv 2>/dev/null | head -n 1 || true)"
  if [[ "$CSV_FILE" == "" ]]; then
    echo "No log found in logs/"
    exit 1
  fi
else
  CSV_FILE="$1"
fi

chmod +x summarize_log.py
./summarize_log.py "$CSV_FILE"
