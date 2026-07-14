#!/bin/zsh
set -euo pipefail
cd "${0:A:h}"
[[ -d dist/MacPowerLab.app ]] || ./scripts/build_swiftui_app.sh
open dist/MacPowerLab.app
