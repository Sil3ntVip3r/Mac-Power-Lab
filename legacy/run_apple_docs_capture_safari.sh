#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x apple_docs_capture.py
./apple_docs_capture.py --safari --crawl-discovered --max-discovered 40 "$@"
