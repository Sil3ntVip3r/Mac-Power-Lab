#!/bin/zsh
set -u
source "${0:A:h}/lib.sh"
OUT="${1:-/tmp/macpowerlab_security_diagnostics_$(date +%Y%m%d_%H%M%S).txt}"
{
  echo "MacPowerLab security diagnostics"
  echo "Created: $(date)"
  echo "macOS: $(sw_vers -productVersion 2>/dev/null) ($(sw_vers -buildVersion 2>/dev/null))"
  echo "Architecture: $(uname -m)"
  echo
  for target in "$ROOT/bin/macpowerlab" "$ROOT/dist/MacPowerLab.app"; do
    echo "===== $target ====="
    if [[ ! -e "$target" ]]; then echo "missing"; echo; continue; fi
    ls -ld "$target"
    echo "-- xattr --"; xattr -l "$target" 2>&1 || true
    echo "-- codesign --"; codesign -dv --verbose=4 "$target" 2>&1 || true
    echo "-- verify --"; codesign --verify --deep --strict --verbose=4 "$target" 2>&1 || true
    echo "-- Gatekeeper --"; spctl --assess --type execute --verbose=4 "$target" 2>&1 || true
    echo
  done
  echo "===== recent macOS security log ====="
  log show --last 10m --style compact --predicate '(process == "amfid") OR (process == "syspolicyd") OR (process == "taskgated-helper") OR (eventMessage CONTAINS[c] "macpowerlab")' 2>&1 | tail -300 || true
} | tee "$OUT"
echo "Diagnostics written: $OUT"
