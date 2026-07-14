#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

echo "Applying MacPowerLab v0.9.0 zsh glob hotfix..."

python3 - <<'PY'
from pathlib import Path

p = Path("run_complete_beta_capture.sh")
if p.exists():
    s = p.read_text()
    s = s.replace("BEFORE_COUNT=$(ls logs/mac_power_*.csv 2>/dev/null | wc -l | tr -d ' ')", "BEFORE_COUNT=$(find logs -maxdepth 1 -type f -name 'mac_power_*.csv' ! -name '*_events*' 2>/dev/null | wc -l | tr -d ' ')")
    s = s.replace("AFTER_COUNT=$(ls logs/mac_power_*.csv 2>/dev/null | wc -l | tr -d ' ')", "AFTER_COUNT=$(find logs -maxdepth 1 -type f -name 'mac_power_*.csv' ! -name '*_events*' 2>/dev/null | wc -l | tr -d ' ')")
    p.write_text(s)
PY

echo "Hotfix complete."
echo "Recommended: download/use v0.8.6 instead, but this patch fixes the immediate no-match error."
