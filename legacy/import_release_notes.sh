#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x import_release_notes.py
./import_release_notes.py "$@"
