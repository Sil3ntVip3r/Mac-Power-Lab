#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p logs
WATTS="${1:-}"
NOTE="${2:-manual reading}"
if [[ "$WATTS" == "" ]]; then read "WATTS?Enter external wall-meter watts: "; fi
python3 - "$WATTS" "$NOTE" <<'PY'
import json, sys
from datetime import datetime
from pathlib import Path
record={"timestamp":datetime.now().isoformat(timespec="seconds"),"watts":sys.argv[1],"note":sys.argv[2] if len(sys.argv)>2 else "manual reading"}
path=Path('logs/wall_meter_readings.jsonl')
with path.open('a',encoding='utf-8') as f: f.write(json.dumps(record)+'\n')
print(f"Saved wall-meter reading to {path}: {record['watts']} W - {record['note']}")
PY
