#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
APP="$ROOT/dist/MacPowerLab.app"
DEST_ROOT="${1:-$HOME/Applications}"
DEST="$DEST_ROOT/MacPowerLab.app"
[[ -d "$APP" ]] || ./scripts/build_swiftui_app.sh
mkdir -p "$DEST_ROOT"
rm -rf "$DEST"
cp -R "$APP" "$DEST"
xattr -cr "$DEST"
codesign --force --deep --sign - --timestamp=none "$DEST" >/dev/null
codesign --verify --deep --strict --verbose=2 "$DEST"
echo "Installed: $DEST"
open "$DEST"
