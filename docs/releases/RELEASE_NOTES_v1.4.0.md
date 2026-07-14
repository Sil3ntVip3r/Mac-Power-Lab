# MacPowerLab v1.4.0

## Correctness

- Uses explicit millivolt/milliamp conversion for AppleSmartBattery and adapter
  fields; low-current samples can no longer be inflated by 1,000×.
- Distinguishes `discharging`, `charged`, and `not charging` from active
  charging in `pmset` output.
- Treats present zero-valued process rates as real zero rather than falling back
  to cumulative or raw values.
- Scales CPU and GPU attribution pools proportionally when component estimates
  exceed the measured dynamic-power pool.
- Runs parity collectors concurrently so comparisons represent the same time
  window.

## Concurrency and lifecycle

- Expires stale powermetrics and process snapshots and reports source status.
- Removes closed powermetrics channels from the manager select loop.
- Uses a latest-value channel policy for system telemetry so a slow consumer
  cannot block the privileged collector.
- Adds an internal single-run benchmark guard, unique workload log directories,
  command persistence, and accurate preparation/terminal progress states.
- Propagates monitor shutdown errors before generating reports or archives.

## Storage, parsing, reporting, and packaging

- Disables an optional SQLite mirror after its first runtime failure while
  continuing canonical JSONL persistence.
- Adds bounded plist depth/node validation, duplicate-key rejection, and strict
  root/trailing-data validation.
- Validates sample schema, session identity, and timestamp ordering during report
  generation; collector errors are included in report warnings.
- Verifies archive file identity after open to close the check/open race.
- Validates the actual bound API listener is loopback-only and uses one server
  shutdown owner.

## Performance

- Prunes temperature history in place rather than allocating a new slice every
  sample.
- Preserves deterministic sensor alias priority with an exact-key fast path.
- No `sync.Pool` was introduced because measured allocation pressure is low at
  production sampling cadences and pooling large plist buffers would retain peak
  memory.
