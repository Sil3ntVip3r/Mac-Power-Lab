#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x compare_logs.py
./compare_logs.py "$@"
