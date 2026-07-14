#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

echo "MacPowerLab v0.9.0 Battery Benchmark Suite"
echo "=========================================="
echo
echo "This creates a repeatable benchmark-style run."
echo "Recommended: start from 60-90% battery, close unnecessary apps, and keep the same brightness for comparisons."
echo

if [[ "${MACPOWERLAB_SKIP_MONITOR_AUTOSTART:-0}" != "1" ]]; then
  ./ensure_power_monitor_running.sh 60
fi

if [[ "${MACPOWERLAB_NO_PROMPT:-0}" != "1" ]]; then
  read "START?Press Enter to start benchmark, or Ctrl+C to cancel..."
fi

./set_phase.sh "BENCHMARK: battery idle baseline"
echo "Battery idle baseline: 120s. Unplug charger if you want a true battery test."
sleep 120

./set_phase.sh "BENCHMARK: CPU load"
./run_cpu_stress_pretty.sh 120

./set_phase.sh "BENCHMARK: GPU load"
./run_gpu_stress_pretty.sh 120 high

./set_phase.sh "BENCHMARK: memory load"
./run_memory_stress_pretty.sh 120

./set_phase.sh "BENCHMARK: extreme load"
./run_max_power_extreme_pretty.sh 180

./clear_phase.sh

if [[ "${MACPOWERLAB_SKIP_POSTPROCESS:-0}" != "1" ]]; then
  ./generate_test_run_summary.sh || true
  ./generate_battery_scorecard.sh || true
  ./generate_report.sh || true
  ./pack_logs.sh || true
fi

echo
echo "Benchmark suite complete."
