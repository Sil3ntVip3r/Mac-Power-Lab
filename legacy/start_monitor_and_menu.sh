#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

./ensure_power_monitor_running.sh 60

if [[ -x ./run_pretty_test_menu.sh ]]; then
  ./run_pretty_test_menu.sh
elif [[ -x ./run_macpowerlab_menu.sh ]]; then
  ./run_macpowerlab_menu.sh
else
  echo "Monitor started. Available benchmark commands:"
  echo "  ./run_battery_discharge_benchmark.sh"
  echo "  ./run_ac_adapter_benchmark.sh"
  echo "  ./run_complete_beta_capture.sh"
fi
