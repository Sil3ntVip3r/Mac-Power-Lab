#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

rm -rf __pycache__ tests/__pycache__
python3 -m compileall -q .
python3 -m unittest discover -s tests -v
python3 mac_power_watch.py --help >/dev/null
python3 app_power_watch.py \
  --top 3 \
  --total-watts 50 \
  --baseline-watts 10 \
  --power-source "Battery Power" \
  --no-bundle-resolution \
  --json >/dev/null

echo "MacPowerLab v0.9.0 validation passed."
