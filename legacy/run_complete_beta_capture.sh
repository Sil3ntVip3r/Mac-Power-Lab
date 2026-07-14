#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

echo "MacPowerLab v0.9.0 Complete Beta Research + Benchmark Capture"
echo "============================================================="
echo

mkdir -p logs exports

echo "Step 1: Starting Mac Power Monitor automatically..."
./ensure_power_monitor_running.sh 75

echo
echo "Step 2: Capturing developer research..."
./run_developer_research_suite.sh || true

echo
echo "Step 3: Capturing macOS sensor scans..."
./run_macos_sensor_scan.sh || true
./run_system_sensor_snapshot.sh || true

echo
echo "Step 4: Running battery benchmark suite..."
MACPOWERLAB_NO_PROMPT=1 MACPOWERLAB_SKIP_MONITOR_AUTOSTART=1 MACPOWERLAB_SKIP_POSTPROCESS=1 ./run_battery_benchmark.sh || true

echo
echo "Step 5: Generating reports and packing logs..."
./generate_test_run_summary.sh || true
./generate_battery_scorecard.sh || true
./generate_report.sh || true
./generate_beta_compatibility_report.sh || true
./pack_logs.sh || true

echo
echo "Complete capture finished."
echo "Upload the newest archive from exports/."
