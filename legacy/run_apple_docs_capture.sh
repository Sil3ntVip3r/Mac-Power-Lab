#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x apple_docs_capture.py
./apple_docs_capture.py "$@"
