#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

echo "MacPowerLab v0.9.0 Battery Discharge Benchmark"
echo "=============================================="
echo
echo "Use this unplugged. It measures real battery discharge watts and runtime behavior under CPU/GPU/memory/extreme workloads."
echo

if pmset -g batt | grep -q "AC Power"; then
  echo "Mac is currently on AC Power."
  echo "Unplug the charger now for a true battery-discharge benchmark."
  read "CONT?Press Enter after unplugging, or Ctrl+C to stop..."
fi

if pmset -g batt | grep -q "AC Power"; then
  echo "Still on AC Power. Stopping to avoid mislabeling the benchmark."
  exit 1
fi

echo
echo "Starting Mac Power Monitor before the battery benchmark..."
./ensure_power_monitor_running.sh 60

MACPOWERLAB_NO_PROMPT=1 MACPOWERLAB_SKIP_MONITOR_AUTOSTART=1 ./run_battery_benchmark.sh
