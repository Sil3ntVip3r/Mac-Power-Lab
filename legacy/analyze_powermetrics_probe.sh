#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x analyze_powermetrics_probe.py
./analyze_powermetrics_probe.py "$@"
