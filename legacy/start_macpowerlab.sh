#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

chmod +x *.sh *.py
./build_tools.sh
./open_power_monitor_window.sh
sleep 1
./run_pretty_test_menu.sh
