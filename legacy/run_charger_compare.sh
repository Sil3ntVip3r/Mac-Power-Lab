#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
echo "Charger/cable comparison workflow"
echo "This records metadata, then starts the monitor."
echo "Open a second Terminal to run:"
echo "  ./run_max_power_test.sh 300"
echo
./set_test_metadata.sh
echo
echo "Starting monitor with powermetrics. After the run, generate report with:"
echo "  ./generate_report.sh"
echo
./run_power_monitor_powermetrics.sh
