#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

SECONDS_TO_RUN="${1:-300}"
GPU_PROFILE="${2:-high}"
MEMORY_MB="${3:-auto}"

if [[ ! -x ./cpu_stress || ! -x ./gpu_stress || ! -x ./memory_stress ]]; then
  ./build_tools.sh
fi

if [[ "$GPU_PROFILE" == "extreme" ]]; then
  TEST_NAME="EXTREME POWER TEST - no SSD writes"
  PHASE_NAME="EXTREME POWER: CPU + extreme GPU + memory"
else
  TEST_NAME="MAX POWER TEST - no SSD writes"
  PHASE_NAME="MAX POWER: CPU + GPU + memory"
fi

echo "$TEST_NAME"
echo "Duration: $SECONDS_TO_RUN seconds"
echo "GPU profile: $GPU_PROFILE"
echo "Memory: $MEMORY_MB"
echo
echo "For maximum real-world draw:"
echo "1. Set screen brightness to 100%."
echo "2. Keep vents clear."
echo "3. Run the monitor in another Terminal:"
echo "   ./run_power_monitor_powermetrics.sh"
echo "4. After test, generate report:"
echo "   ./generate_report.sh"
echo
if [[ "${MACPOWERLAB_AUTO_START:-0}" == "1" || "${MACPOWERLAB_PRETTY_UI:-0}" == "1" ]]; then
  echo "Auto-start enabled by MacPowerLab Test UI."
else
  read "REPLY?Press Enter to start max power test, or Ctrl+C to cancel..."
fi

./set_phase.sh "$PHASE_NAME"

cleanup() {
  echo
  echo "Stopping max power test..."
  for p in $(jobs -p); do kill "$p" 2>/dev/null || true; done
  pkill -x cpu_stress 2>/dev/null || true
  pkill -x gpu_stress 2>/dev/null || true
  pkill -x memory_stress 2>/dev/null || true
  ./clear_phase.sh >/dev/null 2>&1 || true
}
trap cleanup INT TERM EXIT

./cpu_stress "$SECONDS_TO_RUN" &
./gpu_stress "$SECONDS_TO_RUN" "$GPU_PROFILE" &
if [[ "$MEMORY_MB" == "auto" ]]; then
  ./memory_stress "$SECONDS_TO_RUN" &
else
  ./memory_stress "$SECONDS_TO_RUN" "$MEMORY_MB" &
fi

wait
