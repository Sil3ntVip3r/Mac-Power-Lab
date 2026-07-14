#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

echo "MacPowerLab v0.9.0 Developer Research Suite"
echo "==========================================="
echo

./run_xcode_guide_capture.sh --max-links 80 || true

LATEST_GUIDE_DIR=$(find logs -maxdepth 1 -type d -name "xcode_guide_capture_*" -print0 2>/dev/null | xargs -0 ls -td 2>/dev/null | head -1 || true)
if [[ -n "$LATEST_GUIDE_DIR" && -f "$LATEST_GUIDE_DIR/APPLE_DOCS_SEED_URLS.txt" ]]; then
  echo
  echo "Using Xcode Guide Apple seed URLs:"
  echo "  $LATEST_GUIDE_DIR/APPLE_DOCS_SEED_URLS.txt"
  ./run_apple_docs_capture.sh --urls-file "$LATEST_GUIDE_DIR/APPLE_DOCS_SEED_URLS.txt" --crawl-discovered --max-discovered 80 || true
else
  echo "No Xcode Guide seed URLs found. Running default Apple docs capture."
  ./run_apple_docs_capture.sh --crawl-discovered --max-discovered 80 || true
fi

./run_xcode_environment_scan.sh || true

LATEST_APPLE_DIR=$(find logs -maxdepth 1 -type d -name "apple_docs_capture_*" -print0 2>/dev/null | xargs -0 ls -td 2>/dev/null | head -1 || true)
if [[ -n "$LATEST_APPLE_DIR" && -f "$LATEST_APPLE_DIR/POWER_RELEVANT_LINES.txt" ]]; then
  if grep -q "No keyword matches found" "$LATEST_APPLE_DIR/POWER_RELEVANT_LINES.txt"; then
    echo
    echo "Apple docs raw capture had no useful keyword hits."
    echo "Trying Safari-rendered capture as fallback..."
    ./run_apple_docs_capture_safari.sh || true
  fi
fi

./generate_beta_compatibility_report.sh || true

echo
echo "Developer research suite complete."
echo "Pack logs with:"
echo "  ./pack_logs.sh"
