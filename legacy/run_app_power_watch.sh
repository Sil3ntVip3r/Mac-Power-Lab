#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Refreshing sudo permission for powermetrics..."
  sudo -v
fi

chmod +x app_power_watch.py
./app_power_watch.py "$@"
