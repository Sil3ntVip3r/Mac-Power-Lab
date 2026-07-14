#!/bin/zsh
set -euo pipefail
source "${0:A:h}/lib.sh"
require go
UNFORMATTED=$(gofmt -l cmd internal || true)
[[ -z "$UNFORMATTED" ]] || { echo "$UNFORMATTED"; exit 1; }
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o /tmp/macpowerlab-darwin-arm64 ./cmd/macpowerlab
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o /tmp/macpowerlab-darwin-amd64 ./cmd/macpowerlab
rm -f /tmp/macpowerlab-darwin-arm64 /tmp/macpowerlab-darwin-amd64
if command -v swiftc >/dev/null 2>&1; then swiftc -frontend -parse swiftui/Sources/MacPowerLabApp/*.swift; fi
echo "All validation passed."
