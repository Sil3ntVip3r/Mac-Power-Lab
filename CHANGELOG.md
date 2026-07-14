# Changelog

All notable changes to Mac Power Lab are documented here. Detailed historical notes are available in [`docs/releases`](docs/releases/).

## [Unreleased]

### Fixed
- Made **Generate Report** visibly show progress, open the generated HTML report, and retain actions to reopen it or reveal it in Finder.
- Added a longer report request timeout for large monitoring sessions and clear errors when the generated artifact cannot be found.
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
