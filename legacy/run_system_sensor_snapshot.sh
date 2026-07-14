#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x system_sensor_snapshot.py
./system_sensor_snapshot.py "$@"
