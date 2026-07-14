#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x generate_app_power_report.py
./generate_app_power_report.py "$@"
