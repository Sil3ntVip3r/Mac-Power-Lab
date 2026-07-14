#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
[[ "$(uname -s)" == "Darwin" ]] || { echo "Run on macOS." >&2; exit 1; }
trap './scripts/diagnose_macos_security.sh || true' ZERR
./scripts/prepare_macos_security.sh
./scripts/run_tests.sh
./scripts/build_macos.sh
./scripts/build_native.sh
sudo -v
./bin/macpowerlab sensors scan >/tmp/macpowerlab_sensor_scan.json
./bin/macpowerlab parity --iterations 3
./bin/macpowerlab monitor --safe --duration 8s >/tmp/macpowerlab_monitor_safe.log
./bin/macpowerlab monitor --duration 12s >/tmp/macpowerlab_monitor_full.log
echo "Live Mac validation complete."
