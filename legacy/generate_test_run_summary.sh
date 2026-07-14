#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x generate_test_run_summary.py
./generate_test_run_summary.py "$@"
