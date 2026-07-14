#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"
echo "MacPowerLab test metadata"
read "CHARGER?Charger name/model: "
read "CABLE?Cable/MagSafe/dock used: "
read "PORT?Port used: "
read "BRIGHTNESS?Display brightness note: "
read "NOTES?Extra notes: "
python3 - "$CHARGER" "$CABLE" "$PORT" "$BRIGHTNESS" "$NOTES" <<'PY'
import json, sys
from datetime import datetime
from pathlib import Path
keys=['charger','cable_or_dock','port','brightness','notes']
data=dict(zip(keys, sys.argv[1:]))
data['created_at']=datetime.now().isoformat(timespec='seconds')
Path('current_test_metadata.json').write_text(json.dumps(data,indent=2),encoding='utf-8')
print('Saved current_test_metadata.json')
PY
