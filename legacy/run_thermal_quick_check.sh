#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p logs

STAMP=$(date +%Y%m%d_%H%M%S)
OUT="logs/thermal_quick_check_${STAMP}.txt"

echo "MacPowerLab v0.9.0 Thermal Quick Check" | tee "$OUT"
echo "Created: $(date)" | tee -a "$OUT"
echo | tee -a "$OUT"

echo "pmset -g therm, max 5 seconds:" | tee -a "$OUT"
( pmset -g therm >> "$OUT" 2>&1 ) &
PID=$!
sleep 5
if kill -0 "$PID" 2>/dev/null; then
  echo "[MacPowerLab] pmset -g therm did not finish in 5s; stopping." | tee -a "$OUT"
  kill "$PID" 2>/dev/null || true
fi
wait "$PID" 2>/dev/null || true

echo | tee -a "$OUT"
echo "thermal-related sysctl values:" | tee -a "$OUT"
sysctl -a 2>/dev/null | grep -Ei 'thermal|temperature|cpufrequency|machdep.cpu|hw.cpufrequency' >> "$OUT" || true

echo
echo "Thermal quick check written:"
echo "  $OUT"
