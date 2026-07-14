#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
require go
mkdir -p bin/linux-amd64 bin/darwin-arm64 bin/darwin-amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/linux-amd64/macpowerlab ./cmd/macpowerlab
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o bin/darwin-arm64/macpowerlab ./cmd/macpowerlab
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/darwin-amd64/macpowerlab ./cmd/macpowerlab
cp bin/linux-amd64/macpowerlab bin/macpowerlab 2>/dev/null || true
echo "Go builds complete."
