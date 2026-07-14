#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
[[ "$(uname -s)" == "Darwin" ]] || { echo "Requires macOS." >&2; exit 1; }

echo "MacPowerLab v1.4.0 local bootstrap"
echo "================================="
echo "This removes quarantine only from this project, signs local outputs,"
echo "validates the CLI, builds native workloads, and creates the app."
echo
rm -rf "$ROOT/dist/MacPowerLab.app"
./scripts/prepare_macos_security.sh
./scripts/build_macos.sh
./scripts/run_tests.sh
./scripts/build_native.sh
./scripts/build_swiftui_app.sh

echo
echo "Bootstrap complete."
echo "open \"$ROOT/dist/MacPowerLab.app\""
