#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

STAMP="$(date +%Y%m%d_%H%M%S)"
OUT="logs/macos_sensor_scan_${STAMP}"
mkdir -p "$OUT"

echo "MacPowerLab macOS sensor scan"
echo "Output folder: $OUT"
echo

sw_vers > "$OUT/sw_vers.txt" 2>&1 || true
uname -a > "$OUT/uname.txt" 2>&1 || true
sysctl -a > "$OUT/sysctl_all.txt" 2>&1 || true
pmset -g batt > "$OUT/pmset_batt.txt" 2>&1 || true
pmset -g custom > "$OUT/pmset_custom.txt" 2>&1 || true
pmset -g cap > "$OUT/pmset_cap.txt" 2>&1 || true
system_profiler SPPowerDataType -json > "$OUT/system_profiler_power.json" 2>&1 || true
system_profiler SPPowerDataType > "$OUT/system_profiler_power.txt" 2>&1 || true
ioreg -r -c AppleSmartBattery -a > "$OUT/apple_smart_battery.plist" 2>&1 || true
ioreg -l -w0 -r -c AppleSmartBattery > "$OUT/apple_smart_battery_raw.txt" 2>&1 || true

if command -v powermetrics >/dev/null 2>&1; then
  echo "powermetrics needs sudo. You may be asked for your password."
  sudo -v || true
  sudo powermetrics -n 1 -i 1000 > "$OUT/powermetrics_raw.txt" 2>&1 || true
else
  echo "powermetrics not found" > "$OUT/powermetrics_raw.txt"
fi

chmod +x analyze_macos_sensor_scan.py
./analyze_macos_sensor_scan.py "$OUT"

echo
echo "Packing all logs with max compression..."
chmod +x pack_logs.py
./pack_logs.py --mode all --format xz --name "mac_power_logs_with_macos_scan_${STAMP}"

echo
echo "Sensor scan complete."
echo "Report:"
echo "  $OUT/macos_sensor_report.txt"
echo "Archive to send/upload:"
ls -t exports/mac_power_logs_with_macos_scan_${STAMP}.tar.xz 2>/dev/null | head -n 1
