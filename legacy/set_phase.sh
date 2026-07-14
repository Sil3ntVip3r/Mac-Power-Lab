#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
PHASE="${1:-idle / unmarked}"
echo "$PHASE" > current_phase.txt
echo "Phase set to: $PHASE"
