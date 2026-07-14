# MacPowerLab Application Power Attribution Architecture

## 1. Data acquisition

`PowermetricsTaskSampler` executes a bounded, one-sample `powermetrics` command
using plist output and the `tasks` sampler. It requests resource-coalition,
Energy Impact, normalized CPU, GPU, I/O, network, and wakeup fields when the
current macOS build supports them. AMP/IPC fields are intentionally omitted from
the frequent app sample to control plist size and measurement overhead. The
sampler progressively falls back to simpler supported option sets.

Resource coalitions are preferred over individual PIDs because they group an
application with XPC/helper processes and account for short-lived processes. If
coalition data is unavailable, task records are grouped by macOS responsible PID
when that relationship is exposed.

## 2. Compatibility layer

The parser accepts field-name variants observed across macOS releases. Numeric
values are validated as finite before use. NUL-separated plist samples are
parsed independently, and malformed samples are skipped. Aggregate `ALL_TASKS`
rows are excluded to prevent double counting; `DEAD_TASKS` remains visible as
unattributed transient activity.

## 3. Name resolution and classification

`BundleNameResolver` uses a bounded cache, known identifiers, application paths,
and optional short-timeout Spotlight lookup. `AppClassifier` separates user
apps, background services, system services, benchmark workloads, measurement
overhead, and unattributed work.

## 4. Attribution model

`AppPowerAttributionEngine` takes the latest activity distribution and the best
MacPowerLab total-power signal:

1. Battery discharge watts
2. BMS SystemPower
3. PowerTelemetry SystemEffectiveTotalLoad
4. powermetrics SoC/component estimate

Apple Energy Impact is used as the primary proportional score. When it is not
available, a documented composite score uses CPU/GPU time, wakeups, disk I/O,
and network I/O. Interrupt/package-idle wakeups, disk bytes, and network bytes
are retained in the per-app session summaries and benchmark-window reports.

Component-aware allocation first assigns baseline-adjusted CPU watts by CPU time
and GPU watts by GPU time. Remaining dynamic power is allocated by Energy Impact.
The allocation is normalized so estimated app shares never exceed the selected
total system-power envelope.

Two main estimates are emitted:

- `estimated_share_w`: proportional share of total system power
- `estimated_dynamic_w`: proportional share of power above the quiet baseline

The dynamic estimate is generally the better indicator of power caused by an
application. The total-share estimate is useful for session energy accounting,
but it necessarily allocates some non-process platform power proportionally.

## 5. Baseline and confidence

A baseline can come from the monitor's explicit initial baseline or a bounded
20th-percentile history of quiet samples for the same power source and total
power method. A zero placeholder baseline is not considered calibrated.

Confidence levels:

- **High**: coalition Energy Impact, high-trust total power, established baseline
- **Medium**: powermetrics task/coalition data with a supported total estimate
- **Low**: process-table fallback or weak total-power source

## 6. Concurrency and performance

`AppPowerWorker` owns one daemon sampling thread and publishes one atomic latest
snapshot. The default app sample interval is 10 seconds with a 1-second window.
The main monitor recalculates estimated watts each refresh using the latest
activity shares, while the session logger writes only once per unique app sample.

Resource usage is bounded by:

- maximum retained activities
- top-app CSV slots
- bounded quiet-baseline history
- bounded name-resolution cache
- incremental JSONL writes
- periodic atomic summary replacement

## 7. Persistence and reporting

`AppPowerSessionLogger` writes:

- streaming time-series JSONL
- incremental JSON summary
- incremental CSV summary

The report generator correlates app samples with benchmark windows and produces
Markdown and self-contained HTML reports.

## 8. Failure isolation

Application attribution is optional and isolated from the battery monitor. If
powermetrics fails, sudo expires, output changes, or a sample times out, the
worker falls back to a low-confidence `ps` sampler. Any app-attribution failure
is represented in status/error fields and does not stop battery logging.
