# Runtime settings

MacPowerLab uses the versioned `macpowerlab.runtime_settings.v1` document for its effective monitoring configuration. The default remains local-only and preserves the original v1.4 collection behavior.

## Profiles

| Profile | UI | Battery | powermetrics | Apps | Durable logging | Nice |
|---|---:|---:|---:|---:|---:|---:|
| Default | 1s | 1s | 2s | 10s | 1s | 0 |
| High responsiveness | 500ms | 500ms | 1s | 2s | 1s | -5 |
| Balanced | 1s | 2s | 3s | 10s | 5s | 0 |
| Low overhead | 2s | 5s | 10s | 30s | 30s | +10 |
| Live only | 1s | 1s | 2s | 10s | Off | 0 |
| Custom | Individually configured within the supported ranges | | | | | |

Changing any preset value produces the `custom` profile. Profile identifiers in JSON use `default`, `high_responsiveness`, `balanced`, `low_overhead`, `live_only`, and `custom`.

## Validation ranges

- UI refresh: 500ms through 60s
- Battery collection: 500ms through 60s
- System `powermetrics`: 1s through 60s
- App attribution: 2s through 60s
- Durable logging: off, or 500ms through 60s
- Ordinary process nice: -5 through +10

Durations are integer milliseconds in JSON. Logging off requires both `logging_enabled: false` and `log_interval_ms: 0`. Named presets must exactly match their published values; modified values must use `profile: "custom"`. Unknown fields and multiple JSON values are rejected.

## Persistence and session boundaries

The settings document is stored at:

```text
<data-dir>/runtime-settings.json
```

It is written through a same-directory temporary file, synchronized, atomically renamed, and followed by a directory sync. The file is `0600`; the data directory is `0700`. Loads reject symlinks, non-regular files, non-private permissions, oversized documents, and schema or range violations.

Every new monitoring session records its complete effective runtime settings in `session.json`. Applying changed settings while monitoring:

1. rejects the request if a benchmark is running;
2. flushes the latest pending durable sample;
3. closes the current session;
4. starts a fresh session with the requested settings;
5. atomically saves the settings; and
6. rolls back to the prior configuration in another fresh session if start or persistence fails.

This prevents one session from containing samples produced under different runtime semantics.

## Live and durable cadence

UI refresh, battery collection, system `powermetrics`, app attribution, and durable logging are independent. A faster live cadence does not force every live sample into storage.

After a durable write, MacPowerLab retains only one deep-copied pending sample. New live samples replace it until the next log boundary. The pending sample is flushed before report snapshots, shutdown, runtime-profile restarts, and benchmark phase transitions. Routine buffer synchronization does not flush it early.

Live-only mode continues live collection and display while leaving `samples.jsonl` and `apps.jsonl` empty. Session metadata, events, and benchmark phase records still persist.

## CLI overrides

The `monitor`, `apps`, `benchmark`, and `serve` commands load persisted settings from the selected data directory, then apply explicit flags:

```text
--profile
--ui-refresh
--battery-interval
--powermetrics-interval
--process-interval
--logging
--log-interval
--process-nice
```

The legacy `--interval` flag remains supported and overrides both UI refresh and battery collection. CLI overrides affect the current process only; the settings API is the persistence path.

Examples:

```bash
./bin/macpowerlab monitor --profile high-responsiveness
./bin/macpowerlab monitor --profile balanced --log-interval 10s
./bin/macpowerlab monitor --profile live-only
./bin/macpowerlab serve --ui-refresh 500ms --battery-interval 2s
```

## Local settings API

All routes except health require the existing bearer token and remain loopback-only:

- `GET /settings` returns the effective document, including startup CLI overrides.
- `GET /settings/profiles` returns the stable profile catalog and complete preset values.
- `PUT /settings` strictly validates, applies, and persists a complete settings document.

`PUT /settings` returns HTTP `409 Conflict` while a benchmark is active.

## Process priority

MacPowerLab uses ordinary Unix niceness only. Negative values favor monitor responsiveness and may require cached sudo authorization; positive values reduce monitor priority. It never enables real-time, deadline, or other kernel scheduling policies.

Before each native benchmark child starts, the backend briefly normalizes its own ordinary nice value to `0`, launches the child, and immediately restores the configured backend value. This keeps runtime profiles from changing benchmark workload priority.


## Historical reports and Live-only mode

Live-only mode intentionally does not create power or application history.
Historical report generation is unavailable until durable logging is enabled and
a new reportable session begins.

## Session provenance

Every new session records the complete runtime settings plus additive effective
collection options: application attribution state, retained application count,
SQLite mirror state, and safe mode. This makes sessions produced by CLI
overrides reproducible and comparable.
