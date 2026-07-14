# Six-phase migration status

## Phase 1 — Stable contracts: complete

- Versioned Go domain models.
- JSON Schema contracts under `contracts/v1`.
- Frozen v0.9.0 legacy implementation under `legacy/`.
- Golden fixtures and schema tests.

## Phase 2 — Offline Go parser: complete

- Dependency-free XML plist parser.
- NUL-delimited `powermetrics` plist parsing.
- Legacy CSV importer.
- Offline `parse` command.

## Phase 3 — Live Go collector and parity: complete

- Long-running `powermetrics` supervisor with restart/backoff.
- AppleSmartBattery and bank parsing.
- Process/coalition attribution.
- JSONL/SQLite persistence.
- Legacy parity command and tolerance report.

## Phase 4 — Go benchmark controller: complete

- Battery, AC, and extreme plans.
- Environment validation and power-source checks.
- Native C/Metal workload builds.
- Active benchmark lock, caffeinate, progress, cancellation, and stopped/failed distinction.

## Phase 5 — Go TUI is default: complete

- Fixed-screen dashboard.
- Battery, charger, component, cluster, thermal, app attribution, warnings, and benchmark progress.
- Compatibility shell commands delegate to the Go binary.

## Phase 6 — SwiftUI application: complete in source and build pipeline

- SwiftUI dashboard, app attribution, benchmark controls, backend launcher, and settings.
- Token-authenticated loopback Go API.
- `.app` bundle build script, ad-hoc signing, and embedded backend/resources.

The SwiftUI and Metal binaries must be linked on macOS with Xcode/Command Line Tools; Linux CI validates Swift source syntax and both Darwin Go cross-builds.
