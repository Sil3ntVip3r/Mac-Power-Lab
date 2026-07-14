#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

ROOT="$(pwd)"

if command -v osascript >/dev/null 2>&1; then
  osascript <<OSA
tell application "Terminal"
  activate
  do script "cd '$ROOT'; ./run_power_monitor_powermetrics_awake.sh"
end tell
OSA
  echo "Power Monitor opened in a new Terminal window."
else
  echo "osascript not found. Start monitor manually:"
  echo "  ./run_power_monitor_powermetrics_awake.sh"
  exit 1
fi
