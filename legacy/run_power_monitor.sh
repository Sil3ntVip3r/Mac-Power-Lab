#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p logs
chmod +x mac_power_watch.py
./mac_power_watch.py --interval 0.5 --system-profiler
