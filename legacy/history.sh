#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x history.py
./history.py
