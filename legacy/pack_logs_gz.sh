#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
chmod +x pack_logs.py
./pack_logs.py --mode all --format gz
