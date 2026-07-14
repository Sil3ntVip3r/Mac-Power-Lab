#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x generate_beta_compatibility_report.py
./generate_beta_compatibility_report.py "$@"
