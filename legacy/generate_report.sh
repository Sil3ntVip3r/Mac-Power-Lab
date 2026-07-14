#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p logs

latest_csv() {
  find logs -maxdepth 1 -type f -name 'mac_power_*.csv' ! -name '*_events*' -print0 2>/dev/null \
    | xargs -0 ls -t 2>/dev/null \
    | head -n 1 || true
}

if [[ -n "${1:-}" ]]; then
  CSV_FILE="$1"
else
  CSV_FILE="$(latest_csv)"
fi

if [[ -z "$CSV_FILE" || ! -f "$CSV_FILE" ]]; then
  echo "No MacPowerLab power CSV found in logs/."
  exit 1
fi

chmod +x generate_report.py generate_app_power_report.py
./generate_report.py "$CSV_FILE"

RUN_STEM="${CSV_FILE:t:r}"
APP_SUMMARY="logs/${RUN_STEM}_apps_summary.json"
if [[ -f "$APP_SUMMARY" ]]; then
  ./generate_app_power_report.py --summary "$APP_SUMMARY"
else
  echo "No app-power summary found for ${RUN_STEM}; main report was generated."
fi
