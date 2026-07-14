#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

echo "MacPowerLab v0.9.0 AC Adapter / Charging Benchmark"
echo "=================================================="
echo
echo "Use this when plugged into a charger. It measures adapter headroom, battery charge acceptance, and whether the battery assists under load."
echo

if ! pmset -g batt | grep -q "AC Power"; then
  echo "WARNING: Mac does not appear to be on AC Power."
  echo "Plug in the charger for an AC adapter benchmark."
  read "CONT?Press Enter to continue anyway, or Ctrl+C to stop..."
fi

echo
echo "Starting Mac Power Monitor before the AC benchmark..."
./ensure_power_monitor_running.sh 60

MACPOWERLAB_NO_PROMPT=1 MACPOWERLAB_SKIP_MONITOR_AUTOSTART=1 ./run_battery_benchmark.sh
