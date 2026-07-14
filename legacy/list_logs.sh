#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

mkdir -p logs exports

show_recent_files() {
  local title="$1"
  local pattern="$2"
  local limit="${3:-10}"
  echo
  echo "$title:"
  find logs -type f -name "$pattern" -print0 2>/dev/null | xargs -0 ls -lt 2>/dev/null | head -n "$limit" || true
}

show_recent_dirs() {
  local title="$1"
  local pattern="$2"
  local limit="${3:-10}"
  echo
  echo "$title:"
  find logs -maxdepth 1 -type d -name "$pattern" -print0 2>/dev/null | xargs -0 ls -ldt 2>/dev/null | head -n "$limit" || true
}

echo "Logs folder:"
echo "  $(pwd)/logs"

show_recent_files "Recent power CSV logs" 'mac_power_*.csv'
show_recent_files "Recent events" '*_events.jsonl'
show_recent_files "Recent debug files" '*_debug.json'
show_recent_files "Recent app-power streams" '*_apps.jsonl'
show_recent_files "Recent app-power summaries" '*_apps_summary.json'
show_recent_files "Recent app-power reports" '*_apps_report.html'
show_recent_files "Recent benchmark summaries" 'test_run_power_summary_*.md'
show_recent_files "Recent battery scorecards" 'battery_benchmark_scorecard_*.md'
show_recent_dirs "Recent macOS sensor scans" 'macos_sensor_scan_*'
show_recent_dirs "Recent system sensor snapshots" 'system_sensor_snapshot_*'
show_recent_dirs "Recent deep sensor probes" 'deep_sensor_probe_*'

echo
echo "Recent exports:"
find exports -maxdepth 1 -type f -name '*.tar.*' -print0 2>/dev/null | xargs -0 ls -lt 2>/dev/null | head -n 10 || true

echo
echo "Active test lock:"
cat logs/.active_test.lock 2>/dev/null || true
