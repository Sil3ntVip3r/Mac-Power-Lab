#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
./run_max_power_test.sh "${1:-300}" extreme "${2:-auto}"
