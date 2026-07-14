#!/bin/zsh
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v xcrun >/dev/null 2>&1; then
  echo "xcrun was not found."
  echo "Install Apple Command Line Tools first:"
  echo "  xcode-select --install"
  exit 1
fi

echo "Building CPU stress tool..."
xcrun clang -O3 -ffast-math -pthread cpu_stress.c -o cpu_stress

echo "Building memory stress tool..."
xcrun clang -O3 memory_stress.c -o memory_stress

echo "Building Metal GPU stress tool..."
xcrun clang -fobjc-arc -framework Foundation -framework Metal -O2 gpu_stress.m -o gpu_stress

chmod +x cpu_stress memory_stress gpu_stress
echo "Done."
