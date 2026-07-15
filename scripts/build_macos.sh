#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"

[[ "$(uname -s)" == "Darwin" ]] || {
  echo "macOS binaries must be built on macOS." >&2
  exit 1
}

require go
require xattr
require codesign
trap 'exit_status=$?; command=${ZSH_DEBUG_CMD:-unknown-command}; echo "${0:t} failed at line ${LINENO}: ${command}" >&2; exit $exit_status' ZERR

echo "[1/5] Clearing quarantine from this project..."
xattr -dr com.apple.quarantine "$ROOT" 2>/dev/null || true

echo "[2/5] Building Darwin binaries..."
mkdir -p bin bin/darwin-arm64 bin/darwin-amd64
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o bin/darwin-arm64/macpowerlab ./cmd/macpowerlab
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/darwin-amd64/macpowerlab ./cmd/macpowerlab

if [[ "${1:-}" == "--universal" ]]; then
  require lipo
  lipo -create bin/darwin-arm64/macpowerlab bin/darwin-amd64/macpowerlab -output bin/macpowerlab
elif [[ "$(uname -m)" == "arm64" ]]; then
  cp bin/darwin-arm64/macpowerlab bin/macpowerlab
else
  cp bin/darwin-amd64/macpowerlab bin/macpowerlab
fi

echo "[3/5] Removing quarantine from binaries..."
for target in bin/macpowerlab bin/darwin-arm64/macpowerlab bin/darwin-amd64/macpowerlab; do
  xattr -d com.apple.quarantine "$target" 2>/dev/null || true
  chmod +x "$target"
done

echo "[4/5] Ad-hoc signing binaries..."
for target in bin/macpowerlab bin/darwin-arm64/macpowerlab bin/darwin-amd64/macpowerlab; do
  codesign --force --sign - --timestamp=none "$target" >/dev/null
  codesign --verify --strict --verbose=1 "$target"
done

echo "[5/5] Running CLI smoke test..."
set +e
SMOKE_OUTPUT=$(./bin/macpowerlab version 2>&1)
SMOKE_STATUS=$?
set -e
if [[ "$SMOKE_STATUS" -ne 0 ]]; then
  echo "MacPowerLab CLI smoke test failed with status $SMOKE_STATUS." >&2
  echo "$SMOKE_OUTPUT" >&2
  xattr -l bin/macpowerlab 2>&1 || true
  codesign -dv --verbose=4 bin/macpowerlab 2>&1 || true
  spctl --assess --type execute --verbose=4 bin/macpowerlab 2>&1 || true
  exit "$SMOKE_STATUS"
fi

echo "$SMOKE_OUTPUT"
echo "Built and verified bin/macpowerlab"
