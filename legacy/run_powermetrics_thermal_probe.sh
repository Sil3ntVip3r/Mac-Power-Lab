#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p logs

STAMP=$(date +%Y%m%d_%H%M%S)
OUT="logs/powermetrics_sampler_probe_${STAMP}.txt"
SUMMARY="logs/powermetrics_sampler_probe_${STAMP}_summary.txt"

DEFAULT_TIMEOUT="${MACPOWERLAB_PROBE_TIMEOUT_SECONDS:-12}"

echo "MacPowerLab v0.9.0 powermetrics sampler probe" | tee "$OUT"
echo "Created: $(date)" | tee -a "$OUT"
echo "Per-command timeout: ${DEFAULT_TIMEOUT}s" | tee -a "$OUT"
echo | tee -a "$OUT"

run_probe_timeout() {
  local title="$1"
  local limit="$2"
  shift 2

  echo "===== $title =====" | tee -a "$OUT"
  echo "\$ $*" | tee -a "$OUT"

  local tmp pid elapsed
  tmp=$(mktemp "/tmp/macpower_probe_${STAMP}_XXXXXX")

  ( "$@" ) > "$tmp" 2>&1 &
  pid=$!
  elapsed=0

  while kill -0 "$pid" 2>/dev/null; do
    if [[ "$elapsed" -ge "$limit" ]]; then
      echo "[MacPowerLab] Timeout after ${limit}s; stopping live/hanging command." | tee -a "$OUT"
      kill "$pid" 2>/dev/null || true
      sleep 1
      kill -9 "$pid" 2>/dev/null || true
      break
    fi
    sleep 1
    elapsed=$(( elapsed + 1 ))
  done

  wait "$pid" 2>/dev/null || true
  cat "$tmp" >> "$OUT" 2>/dev/null || true
  rm -f "$tmp"
  echo | tee -a "$OUT"
}

echo "Refreshing sudo permission if needed..."
sudo -v || true

run_probe_timeout "powermetrics help" 8 powermetrics --help
run_probe_timeout "cpu_power,gpu_power,thermal" 15 sudo powermetrics -n 1 -i 1000 --samplers cpu_power,gpu_power,thermal
run_probe_timeout "thermal only" 15 sudo powermetrics -n 1 -i 1000 --samplers thermal
run_probe_timeout "pmset therm" 6 pmset -g therm

# Optional legacy probe. Disabled by default because this Mac reports:
#   powermetrics: unrecognized sampler: smc
if [[ "${MACPOWERLAB_PROBE_SMC:-0}" == "1" ]]; then
  run_probe_timeout "smc only optional legacy test" 8 sudo powermetrics -n 1 -i 1000 --samplers smc
fi

{
  echo "MacPowerLab powermetrics probe summary"
  echo "Raw file: $OUT"
  echo
  echo "Relevant lines:"
  grep -Ein "sampler|thermal|pressure|frequency|residency|CPU Power|GPU Power|Combined Power|speed limit|scheduler|available cpu|error|invalid|unknown|timeout|missing" "$OUT" || true
} > "$SUMMARY"

./analyze_powermetrics_probe.sh || true

echo
echo "Probe complete."
echo "Raw probe:"
echo "  $OUT"
echo "Summary:"
echo "  $SUMMARY"
echo
echo "Pack logs with:"
echo "  ./pack_logs.sh"
