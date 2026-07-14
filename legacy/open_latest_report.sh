#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p logs

latest_file() {
  local pattern="$1"
  find logs -maxdepth 1 -type f -name "$pattern" -print0 2>/dev/null \
    | xargs -0 ls -t 2>/dev/null \
    | head -n 1 || true
}

latest_main_report() {
  find logs -maxdepth 1 -type f -name '*_report.html' ! -name '*_apps_report.html' -print0 2>/dev/null \
    | xargs -0 ls -t 2>/dev/null \
    | head -n 1 || true
}

MAIN_REPORT="$(latest_main_report)"
if [[ -z "$MAIN_REPORT" ]]; then
  ./generate_report.sh
  MAIN_REPORT="$(latest_main_report)"
fi

if [[ -n "$MAIN_REPORT" && -f "$MAIN_REPORT" ]]; then
  echo "Opening main report: $MAIN_REPORT"
  open "$MAIN_REPORT"
else
  echo "No main report available."
  exit 1
fi

APP_REPORT="$(latest_file '*_apps_report.html')"
if [[ -n "$APP_REPORT" && -f "$APP_REPORT" ]]; then
  echo "Opening application power report: $APP_REPORT"
  open "$APP_REPORT"
fi
