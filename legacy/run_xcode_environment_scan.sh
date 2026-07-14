#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x xcode_environment_scan.py
./xcode_environment_scan.py "$@"
