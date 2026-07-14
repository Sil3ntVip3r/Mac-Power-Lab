#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x list_test_runs.py
./list_test_runs.py
