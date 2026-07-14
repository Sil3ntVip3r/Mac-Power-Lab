# Architecture

## Layering

1. `internal/collector` owns macOS subprocesses and raw sensor parsing.
2. `internal/model` owns stable versioned domain contracts.
3. `internal/attribution` converts process/coalition activity into confidence-labelled power estimates.
4. `internal/store` persists streaming JSONL and an optional SQLite mirror.
5. `internal/benchmark` owns validated plans, native workload builds, locks, cancellation, and progress.
6. `internal/report` and `internal/archive` consume stable contracts without collector dependencies.
7. `internal/server` exposes a loopback-only, bearer-token API to SwiftUI.
8. `internal/tui` renders the terminal dashboard.

Dependencies point inward toward models and contracts. Collectors do not own reporting, benchmark plans do not parse sensor formats, and the SwiftUI app never reads privileged sensors directly.

## Sensor hierarchy

On Battery Power:

1. Battery discharge watts.
2. BMS SystemPower.
3. PowerTelemetry SystemEffectiveTotalLoad.
4. `powermetrics` package/component estimates.

On AC Power, MacPowerLab estimates charger output from system load plus battery charge acceptance, or subtracts battery assist. Exact wall draw still requires external metering.

## Application attribution

The process collector prefers resource coalitions, then responsible PID, then task PID. CPU power is distributed by CPU time, GPU power by GPU time, and residual dynamic power by Apple Energy Impact, wakeups, disk, and network activity. Baseline platform power is learned separately for AC and battery states.

Attribution is explicitly estimated, bounded, streaming, and confidence-labelled.

## Persistence

Canonical records are append-only JSONL. Session metadata and summaries use atomic JSON replacement. SQLite is an optional query mirror, not the source of truth. This allows recovery if SQLite is missing or a process is interrupted.
