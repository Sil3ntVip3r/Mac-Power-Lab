#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
./scripts/bootstrap_macos.sh
./scripts/install_app.sh "${1:-$HOME/Applications}"
