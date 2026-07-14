#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

if command -v caffeinate >/dev/null 2>&1; then
  echo "Running monitor with sleep prevention: caffeinate -dimsu"
  exec caffeinate -dimsu ./run_power_monitor_powermetrics.sh "$@"
else
  echo "caffeinate not found; running monitor normally."
  exec ./run_power_monitor_powermetrics.sh "$@"
fi
