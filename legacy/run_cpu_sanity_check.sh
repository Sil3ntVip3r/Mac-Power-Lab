#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

DURATION="${1:-20}"

echo "MacPowerLab CPU sanity check"
echo "Duration: ${DURATION}s"
echo
echo "This runs CPU stress briefly and samples process CPU with ps."
echo

./run_cpu_stress.sh "$DURATION" &
ROOT_PID=$!

for i in $(seq 1 "$DURATION"); do
  if ! kill -0 "$ROOT_PID" 2>/dev/null; then
    break
  fi

  echo
  echo "Second $i:"

  ps -axo pid,ppid,pcpu,pmem,comm | awk -v root="$ROOT_PID" '
    NR==1 { next }
    $1==root || $2==root || $5 ~ /cpu_stress/ {
      cpu += $3
      mem += $4
      count += 1
      print "  " $0
    }
    END {
      printf("  total matching CPU: %.1f%% across %d process(es)\n", cpu, count)
    }
  '

  sleep 1
done

wait "$ROOT_PID" || true
echo
echo "CPU sanity check complete."
