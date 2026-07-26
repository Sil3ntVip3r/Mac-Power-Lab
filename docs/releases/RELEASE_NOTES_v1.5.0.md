# MacPowerLab v1.5.0

## Runtime control and diagnostics

- Adds versioned runtime profiles with independent live, battery, `powermetrics`,
  application-attribution, and durable-log cadences.
- Adds requested-versus-observed cadence diagnostics to the SwiftUI settings and
  Full Monitor workspaces.
- Preserves responsive Live-only monitoring while allowing durable sample logging
  to be disabled.

## Benchmark lifecycle

- Captures the requested and observed niceness of the backend and every active
  native benchmark workload.
- Uses a bounded phase-completion grace that scales for long-running soak tests.
- Treats successful workload process exits as authoritative when the phase
  deadline races with final log flushing and cleanup.
- Keeps cancellation and failed child exits distinguishable from successful
  completion.

## Reports and support bundles

- Generates immutable cumulative report snapshots without stopping monitoring.
- Adds `macpowerlab support pack` with exclusions for local API credentials,
  private keys, launcher files, transient SQLite sidecars, and Finder metadata.
- Records the actual bundle creation time in `MANIFEST_macpowerlab.json` while
  retaining normalized TAR member and gzip header timestamps.
- Lists every excluded path and reason without opening or packaging excluded
  contents.

## Compatibility

MacPowerLab v1.5.0 preserves the existing v1 JSON schema identifiers and the
local-only, authenticated-loopback, no-telemetry defaults.
