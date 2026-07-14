#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
./scripts/run_tests.sh
./scripts/build_go.sh
mkdir -p dist
NAME="MacPowerLab_v1.0.0"
PARENT="${ROOT:h}"
cd "$PARENT"
COPYFILE_DISABLE=1 tar --no-xattrs -czf "$ROOT/dist/${NAME}.tar.gz" "$NAME"
cd "$ROOT"
python3 - <<'PYSCRIPT'
from pathlib import Path
import zipfile
root=Path('.').resolve(); out=root/'dist'/'MacPowerLab_v1.0.0.zip'
with zipfile.ZipFile(out,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
    for p in sorted(root.rglob('*')):
        if not p.is_file() or '/dist/' in str(p): continue
        z.write(p,Path(root.name)/p.relative_to(root))
PYSCRIPT
shasum -a 256 dist/${NAME}.zip dist/${NAME}.tar.gz > dist/SHA256SUMS.txt
echo "Release archives written to dist/."
