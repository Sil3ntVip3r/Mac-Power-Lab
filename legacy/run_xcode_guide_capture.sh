#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x xcode_guide_capture.py
./xcode_guide_capture.py "$@"
