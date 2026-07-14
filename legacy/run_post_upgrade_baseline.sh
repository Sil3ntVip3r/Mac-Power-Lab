#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION_IDLE="${1:-120}"
DURATION_LOAD="${2:-300}"

echo "MacPowerLab post-upgrade baseline helper"
echo
echo "Recommended before starting:"
echo "1. Plug into charger."
echo "2. Turn Low Power Mode OFF for AC and Battery if you want true max-power data."
echo "3. Set brightness where you want it for comparison."
echo
echo "Step A: Run this in Terminal 1:"
echo "  ./run_power_monitor_powermetrics.sh"
echo
echo "Step B: In Terminal 2, run this helper again with:"
echo "  ./run_post_upgrade_baseline.sh start"
echo

if [[ "${1:-}" != "start" ]]; then
  exit 0
fi

./set_phase.sh "POST UPGRADE: AC idle baseline"
echo "AC idle baseline for ${DURATION_IDLE}s..."
sleep "$DURATION_IDLE"

echo "Now unplug charger for battery idle baseline."
read "OK?Press Enter after unplugging..."
./set_phase.sh "POST UPGRADE: battery idle baseline"
sleep "$DURATION_IDLE"

echo "Starting max load on battery for ${DURATION_LOAD}s..."
./set_phase.sh "POST UPGRADE: max load on battery"
./run_max_power_test.sh "$DURATION_LOAD"

echo "Now plug charger back in while monitor is still running."
read "OK?Press Enter after plugging in..."
./set_phase.sh "POST UPGRADE: max load on AC recovery"
./run_max_power_test.sh "$DURATION_LOAD"

./clear_phase.sh
./generate_report.sh
./pack_logs.sh

echo "Post-upgrade baseline complete."
