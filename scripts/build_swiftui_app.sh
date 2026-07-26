#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"

[[ "$(uname -s)" == "Darwin" ]] || {
  echo "SwiftUI app linking requires macOS." >&2
  exit 1
}

require swiftc
require codesign
require xattr
require plutil

APP="dist/MacPowerLab.app"
CONTENTS="$APP/Contents"
MACOS="$CONTENTS/MacOS"
RES="$CONTENTS/Resources"

cleanup_failed_app() {
  local exit_status=$?
  local command=${ZSH_DEBUG_CMD:-unknown-command}
  echo "build_swiftui_app.sh failed at line ${LINENO}: ${command}" >&2
  echo "Removing incomplete app bundle: $APP" >&2
  rm -rf "$APP"
  exit "$exit_status"
}
trap cleanup_failed_app ZERR

echo "[1/8] Preparing project security attributes..."
xattr -dr com.apple.quarantine "$ROOT" 2>/dev/null || true

echo "[2/8] Building the Go backend..."
./scripts/build_macos.sh

echo "[3/8] Building native benchmark workloads..."
./scripts/build_native.sh

echo "[4/8] Creating app bundle..."
rm -rf "$APP"
mkdir -p "$MACOS" "$RES/bin/native"

echo "[5/8] Compiling SwiftUI frontend..."
swiftc -O -parse-as-library -target "$(uname -m)-apple-macos14.0" \
  -framework SwiftUI -framework AppKit \
  swiftui/Sources/MacPowerLabApp/*.swift \
  -o "$MACOS/MacPowerLabApp"

if [[ ! -x "$MACOS/MacPowerLabApp" ]]; then
  echo "Swift compiler completed without producing the app executable." >&2
  exit 1
fi

echo "[6/8] Copying backend and workloads..."
cp bin/macpowerlab "$RES/macpowerlab"
cp -R native contracts legacy "$RES/"
cp bin/native/cpu_stress bin/native/memory_stress bin/native/gpu_stress "$RES/bin/native/"
chmod +x "$MACOS/MacPowerLabApp" "$RES/macpowerlab" "$RES/bin/native/"*

cat > "$CONTENTS/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>MacPowerLabApp</string>
<key>CFBundleIdentifier</key><string>com.macpowerlab.app</string>
<key>CFBundleName</key><string>MacPowerLab</string>
<key>CFBundleDisplayName</key><string>MacPowerLab</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>1.5.0</string>
<key>CFBundleVersion</key><string>150</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST
plutil -lint "$CONTENTS/Info.plist"

echo "[7/8] Removing quarantine and signing nested executables..."
xattr -cr "$APP"
for target in "$MACOS/MacPowerLabApp" "$RES/macpowerlab" "$RES/bin/native/cpu_stress" "$RES/bin/native/memory_stress" "$RES/bin/native/gpu_stress"; do
  codesign --force --sign - --timestamp=none "$target" >/dev/null
done
codesign --force --sign - --timestamp=none "$APP" >/dev/null

echo "[8/8] Verifying app bundle..."
codesign --verify --deep --strict --verbose=2 "$APP"
xattr -cr "$APP"
[[ -x "$MACOS/MacPowerLabApp" ]] || { echo "App executable missing." >&2; exit 1; }

echo
echo "Built and verified $APP"
echo "Open with: open \"$APP\""
