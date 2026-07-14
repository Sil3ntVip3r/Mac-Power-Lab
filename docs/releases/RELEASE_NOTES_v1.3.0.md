# MacPowerLab v1.3.0

## Principal-level Go reliability refactor

This release preserves the v1 JSON contracts and public CLI/API behavior while
hardening lifecycle management, process supervision, attribution, persistence,
reporting, and archival behavior.

### Concurrency and lifecycle

- Serializes monitor and benchmark transitions in the local API.
- Removes shared handler error variables and benchmark ABA races.
- Ties monitor and benchmark contexts to the server lifetime.
- Waits for process collectors and store flushers before closing channels.
- Rolls back failed monitor startup rather than poisoning the manager lifecycle.
- Keeps a monitor published when a timed shutdown has not actually completed.

### Process execution and memory bounds

- Drains subprocess output after capture limits instead of blocking child pipes.
- Uses process groups and escalation for cancellation.
- Bounds powermetrics NUL frames, task plist output, retained process rows, and
  fallback `ps` rows.
- Parses only the final complete one-shot task plist.
- Removes full raw plist retention from live samples.

### Attribution correctness

- Fixes zero-activity applications receiving non-zero component shares.
- Prevents sustained workloads from contaminating the quiet-load baseline.
- Bounds per-application accumulated energy state with TTL and hard eviction.
- Normalizes cumulative task counters by sample duration.

### Persistence, reports, and archives

- Creates unique session directories atomically.
- Cleans up partially initialized stores and surfaces SQLite errors.
- Uses buffered SQLite writes and bounded shutdown.
- Generates time-weighted reports in one sample pass, skips sleep/wake gaps,
  deduplicates test-run state records, and propagates corrupt-log errors.
- Rejects symlinks and non-regular archive entries, hashes while streaming, and
  publishes archives atomically.

### Testability

- Adds injectable monitor, benchmark, sudo, and command dependencies.
- Adds lifecycle, concurrency, bounded-memory, attribution-invariant, storage,
  report, archive, process-fallback, parity, and TUI regression tests.
