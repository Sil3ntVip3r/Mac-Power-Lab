# Validation

The release pipeline performs:

- `gofmt` verification.
- `go vet ./...`.
- `go test ./...` including race-safe attribution invariants, plist parsing, schema checks, storage/report generation, and CLI smoke tests.
- Targeted race tests for server, collector, benchmark, store, attribution, config, and priority lifecycle code.
- A full `go test -race ./...` pass before release.
- `CGO_ENABLED=0` Darwin arm64 and amd64 cross-builds.
- Linux amd64 build for parser/report CI.
- Swift source syntax parse.
- Archive SHA-256 generation.

Live battery, charger, `powermetrics`, Metal, and SwiftUI linking require a Mac and are validated by `scripts/validate_on_mac.sh`.

Milestone 1 validation commands:

```bash
test -z "$(gofmt -l cmd internal)"
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./internal/server ./internal/collector \
  ./internal/benchmark ./internal/store ./internal/attribution \
  ./internal/config ./internal/priority
GOMAXPROCS=2 go test -race -count=1 -p 1 ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./cmd/macpowerlab
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./cmd/macpowerlab
xcrun swiftc -parse swiftui/Sources/MacPowerLabApp/*.swift
swift build --package-path swiftui
./scripts/bootstrap_macos.sh
```

The final three commands must run on macOS. `scripts/validate_on_mac.sh` additionally exercises sudo-backed sensors, parity, safe monitoring, and full monitoring on real hardware.
