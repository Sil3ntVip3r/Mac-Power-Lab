#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

WAIT_SECONDS="${1:-60}"
FRESH_SECONDS="${MACPOWERLAB_MONITOR_FRESH_SECONDS:-8}"

mkdir -p logs exports

count_power_csvs() {
  find logs -maxdepth 1 -type f -name 'mac_power_*.csv' ! -name '*_events*' 2>/dev/null | wc -l | tr -d ' '
}

latest_power_csv() {
  find logs -maxdepth 1 -type f -name 'mac_power_*.csv' ! -name '*_events*' -print0 2>/dev/null | xargs -0 ls -t 2>/dev/null | head -1 || true
}

csv_age_seconds() {
  local csv="$1"
  if [[ -z "$csv" || ! -f "$csv" ]]; then
    echo 999999
    return
  fi
  local now mod
  now=$(date +%s)
  mod=$(stat -f %m "$csv" 2>/dev/null || echo 0)
  echo $(( now - mod ))
}

is_monitor_fresh() {
  local csv age
  csv=$(latest_power_csv)
  if [[ -z "$csv" ]]; then
    return 1
  fi
  age=$(csv_age_seconds "$csv")
  [[ "$age" -le "$FRESH_SECONDS" ]]
}

if is_monitor_fresh; then
  echo "Mac Power Monitor already appears active:"
  latest_power_csv
  exit 0
fi

BEFORE_COUNT=$(count_power_csvs)

echo "Mac Power Monitor is not writing a fresh CSV yet."
echo "Launching Mac Power Monitor automatically..."

if [[ -x ./open_power_monitor_window.sh ]]; then
  ./open_power_monitor_window.sh || true
else
  echo "open_power_monitor_window.sh not found."
fi

echo
echo "Waiting up to ${WAIT_SECONDS}s for a fresh power log..."
echo "If Terminal asks for your password, enter it in the new Mac Power Monitor window."
echo

for i in $(seq 1 "$WAIT_SECONDS"); do
  if is_monitor_fresh; then
    echo "Mac Power Monitor is active:"
    latest_power_csv
    exit 0
  fi

  # Also accept a brand-new CSV even if mtime check races.
  AFTER_COUNT=$(count_power_csvs)
  if [[ "$AFTER_COUNT" -gt "$BEFORE_COUNT" ]]; then
    csv=$(latest_power_csv)
    if [[ -n "$csv" ]]; then
      age=$(csv_age_seconds "$csv")
      if [[ "$age" -le 15 ]]; then
        echo "Mac Power Monitor started:"
        echo "$csv"
        exit 0
      fi
    fi
  fi

  sleep 1
done

echo
echo "WARNING: Mac Power Monitor did not start a fresh CSV within ${WAIT_SECONDS}s."
echo "Start it manually in another Terminal if needed:"
echo "  cd \"$PWD\""
echo "  ./run_power_monitor_powermetrics_awake.sh"
echo

if [[ "${MACPOWERLAB_REQUIRE_MONITOR:-0}" == "1" ]]; then
  echo "MACPOWERLAB_REQUIRE_MONITOR=1 is set, so stopping."
  exit 1
fi

if [[ "${MACPOWERLAB_NO_PROMPT:-0}" != "1" ]]; then
  read "CONT?Press Enter to continue without a fresh monitor log, or Ctrl+C to stop..."
fi

exit 0
