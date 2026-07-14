#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p logs

echo "Refreshing sudo permission for powermetrics..."
sudo -v

chmod +x mac_power_watch.py
./mac_power_watch.py --powermetrics --app-power --system-profiler --full --interval 0.5 --rolling-window 60 --debug-every 10
