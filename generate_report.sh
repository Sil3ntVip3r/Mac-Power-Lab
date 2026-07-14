#!/bin/zsh
set -euo pipefail
cd "${0:A:h}"
[[ -x bin/macpowerlab ]] || ./scripts/build_macos.sh
exec ./bin/macpowerlab report "$@"
