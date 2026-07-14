#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"

[[ "$(uname -s)" == "Darwin" ]] || {
  echo "This preparation script requires macOS." >&2
  exit 1
}

require xattr
require codesign

echo "Preparing MacPowerLab for local execution..."
echo "Project: $ROOT"
echo

# Scope quarantine removal to this MacPowerLab project. Never disable
# Gatekeeper globally.
if xattr -r "$ROOT" 2>/dev/null | grep -q "com.apple.quarantine"; then
  echo "Removing quarantine attributes from this MacPowerLab folder only..."
  xattr -dr com.apple.quarantine "$ROOT" 2>/dev/null || true
else
  echo "No project quarantine attribute detected."
fi

sign_binary() {
  local target="$1"
  [[ -f "$target" ]] || return 0
  chmod +x "$target"
  xattr -d com.apple.quarantine "$target" 2>/dev/null || true
  codesign --force --sign - --timestamp=none "$target" >/dev/null
  codesign --verify --strict --verbose=1 "$target"
}

for target in \
  bin/macpowerlab \
  bin/darwin-arm64/macpowerlab \
  bin/darwin-amd64/macpowerlab \
  bin/native/cpu_stress \
  bin/native/memory_stress \
  bin/native/gpu_stress
do
  sign_binary "$target"
done

echo
echo "Local security preparation complete."
