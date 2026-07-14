#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"

[[ "$(uname -s)" == "Darwin" ]] || {
  echo "Native workloads require macOS." >&2
  exit 1
}

require xattr
require codesign
trap 'status=$?; command=${ZSH_DEBUG_CMD:-unknown-command}; echo "${0:t} failed at line ${LINENO}: ${command}" >&2; exit $status' ZERR

[[ -x bin/macpowerlab ]] || ./scripts/build_macos.sh
./scripts/prepare_macos_security.sh

echo "Building native CPU, memory, and Metal workloads..."
./bin/macpowerlab build-native

for target in bin/native/cpu_stress bin/native/memory_stress bin/native/gpu_stress; do
  [[ -x "$target" ]] || { echo "Missing native workload: $target" >&2; exit 1; }
  xattr -d com.apple.quarantine "$target" 2>/dev/null || true
  codesign --force --sign - --timestamp=none "$target" >/dev/null
  codesign --verify --strict --verbose=1 "$target"
done

echo "Built and verified native workloads."
