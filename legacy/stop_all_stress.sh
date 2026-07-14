#!/bin/zsh
echo "Stopping local stress tools..."
pkill -x cpu_stress 2>/dev/null || true
pkill -x gpu_stress 2>/dev/null || true
pkill -x memory_stress 2>/dev/null || true
pkill -x yes 2>/dev/null || true
if [[ -f "$(dirname "$0")/current_phase.txt" ]]; then
  echo "idle / unmarked" > "$(dirname "$0")/current_phase.txt"
fi
echo "Done."
