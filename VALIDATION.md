# Validation

The release pipeline performs:

- `gofmt` verification.
- `go vet ./...`.
- `go test ./...` including race-safe attribution invariants, plist parsing, schema checks, storage/report generation, and CLI smoke tests.
- `CGO_ENABLED=0` Darwin arm64 and amd64 cross-builds.
- Linux amd64 build for parser/report CI.
- Swift source syntax parse.
- Archive SHA-256 generation.

Live battery, charger, `powermetrics`, Metal, and SwiftUI linking require a Mac and are validated by `scripts/validate_on_mac.sh`.
