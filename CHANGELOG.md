# Changelog

All notable changes to Mac Power Lab are documented here. Detailed historical notes are available in [`docs/releases`](docs/releases/).

## [Unreleased]

## [1.5.0] - 2026-07-16

### Added
- Added observed cadence diagnostics for SwiftUI polling, backend live publication, battery, `powermetrics`, app attribution, and durable logging.
- Added live benchmark priority observations that record the backend and each running workload process while the phase is active.
- Added `macpowerlab support pack`, with archive-level credential and private-key exclusion.
- Added versioned runtime profiles with independent live, collector, application-attribution, and durable-log cadences.
- Added local-only Live-only mode, strict private settings persistence, settings API/CLI/SwiftUI controls, and conservative ordinary process priority.
- Added complete effective collection options to new session metadata and durable product, UX, privacy, and roadmap documentation.

### Fixed
- Scaled the bounded benchmark phase-completion grace for long soaks and treated successful workload exits as authoritative when a deadline races with final cleanup.
- Recorded the actual support-bundle creation time in `MANIFEST_macpowerlab.json` while retaining normalized TAR member and gzip header timestamps.
- Removed the duplicate store-level cadence calculation so the collector manager is the single owner of durable log deadlines.
- Prevented support bundles from including local API tokens, token-address files, launcher scripts, SQLite sidecars, Finder metadata, or private-key material.
- Made settings restarts transactional so persistence or shutdown failure cannot launch overlapping collectors.
- Disabled misleading historical report generation while durable logging is off and extended the settings-update client timeout.
- Corrected Apple Silicon `powermetrics` CPU, GPU, package, ANE, DRAM, and cluster power fields from milliwatts to watts using explicit source units.
- Treated an empty optional `AppleSmartBatteryBank` result as not present instead of producing a repeated parse warning.
- Generated immutable timestamped report snapshots that remain cumulative from session start and preserve every earlier report.
- Made **Generate Report** visibly show progress, open the generated HTML report, and retain actions to reopen it or reveal it in Finder.
- Displayed valid zero application-attribution values as `0.00 W` instead of `n/a` while preserving `n/a` for unavailable attribution.

## [1.4.0] - 2026-07-13

### Fixed
- Corrected battery electrical unit conversion and charging-state parsing.
- Hardened telemetry freshness, channel closure, and bounded process collection.
- Enforced power-conserving application attribution.
- Strengthened benchmark lifecycle ownership and terminal-state reporting.
- Added graceful SQLite mirror degradation and stronger JSONL/report validation.
- Hardened archive creation against symlinks, special files, and partial publication.
- Improved API shutdown, parity timing, plist bounds, and deterministic field selection.

## [1.3.0] - 2026-07-13

### Changed
- Principal Go concurrency, lifecycle, storage, report, and archive hardening.
- Added narrow dependency interfaces and expanded race/integration coverage.

## [1.2.0] - 2026-07-13

### Added
- Dedicated Overview, Battery & Charging, Performance, Applications, and Full Monitor workspaces.
- Full-height sortable application table with Power, Activity, I/O, and Identity modes.

## [1.1.0] - 2026-07-13

### Added
- Expanded benchmark catalog and validated custom benchmark builder.
- Sortable application attribution columns and richer benchmark explanations.

## [1.0.0] - 2026-07-13

### Added
- Go monitoring engine, native workloads, SwiftUI application, CLI/TUI, local API, versioned contracts, storage, reports, and legacy parity implementation.
