#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-1800}"
MODE="${2:-auto}"

echo "MacPowerLab v0.9.0 Extreme Soak Benchmark"
echo "========================================="
echo
echo "This is the heaviest sustained single benchmark:"
echo "  CPU stress + extreme GPU stress + memory bandwidth"
echo
echo "Duration: ${DURATION}s"
echo "Memory:   ${MODE}"
echo

./ensure_power_monitor_running.sh 75

if [[ "${MACPOWERLAB_NO_PROMPT:-0}" != "1" ]]; then
  echo
  echo "Recommended:"
  echo "- Plugged in: tests charger/headroom/battery assist."
  echo "- Unplugged: tests true battery draw/runtime."
  echo "- Keep vents clear."
  echo "- Use high brightness if you want worst-case real-world draw."
  echo
  read "START?Press Enter to start extreme soak, or Ctrl+C to cancel..."
fi

./set_phase.sh "EXTREME SOAK: CPU + extreme GPU + memory"
./run_max_power_extreme_pretty.sh "$DURATION" "$MODE" || true
./clear_phase.sh || true

./generate_test_run_summary.sh || true
./generate_battery_scorecard.sh || true
./generate_report.sh || true
./pack_logs.sh || true

echo
echo "Extreme soak benchmark finished."
echo "Upload the newest archive from exports/."
