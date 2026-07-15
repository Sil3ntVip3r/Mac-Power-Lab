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
9. `internal/config` owns the versioned runtime-settings contract, presets, strict validation, and private atomic persistence.
10. `internal/priority` owns ordinary macOS niceness and benchmark-child normalization; it exposes no real-time scheduling policy.

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

Live publication and durable persistence are separate paths. The monitor publishes only the latest unread live frame. The store writes the first sample and cadence-due samples, retaining one deep-copied latest pending sample between durable writes. Periodic buffer flushes do not publish that pending sample. Report snapshots, shutdown, runtime-profile restarts, and benchmark phase boundaries explicitly flush it.

Live-only sessions still persist session metadata, events, and benchmark results, but leave canonical power-sample and app-attribution logs empty.

## Runtime settings and restart ownership

The effective `macpowerlab.runtime_settings.v1` document is embedded in every new monitoring session. CLI monitoring commands load `<data-dir>/runtime-settings.json` before applying explicit flags. The SwiftUI app reads and updates the same settings through authenticated loopback endpoints.

The server serializes monitor, benchmark, and settings transitions. It rejects settings changes during an active benchmark. A settings change while monitoring flushes and closes the old session, starts a new monitor with the requested configuration, atomically persists the settings, and publishes the new monitor. A failed start or persistence attempt makes a best-effort rollback to the previous configuration in another fresh session; samples from different effective configurations are never appended to one session.
