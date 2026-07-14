#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
echo "idle / unmarked" > current_phase.txt
echo "Phase cleared."
