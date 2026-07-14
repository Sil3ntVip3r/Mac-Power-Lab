#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x deep_sensor_probe.py
./deep_sensor_probe.py "$@"
