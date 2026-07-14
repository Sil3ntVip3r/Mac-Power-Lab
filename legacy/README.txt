MacPowerLab v0.9.0
====================

Power-first Mac battery, charging, benchmark, thermal, and application-energy monitor.

NEW IN v0.9.0
-------------

MacPowerLab now records which applications and system services are associated
with the Mac's power use while the live monitor or benchmark is running.

The application subsystem uses:

- Apple powermetrics task/resource-coalition data
- Energy Impact
- CPU and GPU time
- interrupt and package-idle wakeups
- disk reads/writes
- network receive/transmit activity
- MacPowerLab's total system power and quiet baseline
- component-aware allocation of CPU/GPU watts when those sensors are usable
- responsible-PID grouping when coalition data is unavailable

macOS does not expose direct electrical watts for each process. MacPowerLab
therefore reports transparent estimates:

- Estimated total share W
- Estimated dynamic W above quiet baseline
- Energy share percent
- Estimated Wh over the run
- Apple Energy Impact
- CPU/GPU activity, wakeups, disk, and network use
- Confidence: high, medium, or low

Resource coalitions are preferred because they group an application with helper
processes and account for short-lived tasks more accurately than PID-only data.

QUICK START — LIVE MONITOR
--------------------------

  cd ~/Downloads/MacPowerLab_v0.9.0
  chmod +x *.sh *.py
  ./build_tools.sh
  ./run_power_monitor_powermetrics_awake.sh

Application power attribution is enabled automatically by the powermetrics
monitor wrapper. The live dashboard shows the top applications and their
estimated watts/dynamic watts.

Open the monitor in a new Terminal window:

  ./open_power_monitor_window.sh

APPLICATION POWER REPORTS
-------------------------

After a monitor or benchmark run:

  ./generate_app_power_report.sh

Outputs:

  logs/mac_power_YYYYMMDD_HHMMSS_apps.jsonl
  logs/mac_power_YYYYMMDD_HHMMSS_apps_summary.json
  logs/mac_power_YYYYMMDD_HHMMSS_apps_summary.csv
  logs/mac_power_YYYYMMDD_HHMMSS_apps_report.md
  logs/mac_power_YYYYMMDD_HHMMSS_apps_report.html

The report includes:

- Top applications and services for the full session
- User applications
- System/background services
- Benchmark workloads
- Per-test application attribution using test_runs.jsonl timestamps
- Estimated Wh, average/peak watts, dynamic Wh, Energy Impact, CPU/GPU,
  disk, network, and wakeup-related telemetry

ONE-SHOT APP DIAGNOSTIC
-----------------------

  ./run_app_power_watch.sh --top 15

For estimated watts in a one-shot sample, provide a known total and baseline:

  ./run_app_power_watch.sh \
    --top 15 \
    --total-watts 65 \
    --baseline-watts 18 \
    --power-source "Battery Power"

BENCHMARKS
----------

Unplugged battery benchmark:

  ./run_battery_discharge_benchmark.sh

Plugged-in charger/load benchmark:

  ./run_ac_adapter_benchmark.sh

Extreme sustained soak:

  ./run_extreme_soak_benchmark.sh 900

Benchmark wrappers now generate the application power report automatically.

LOG EXPORT
----------

Pack all logs with maximum compression:

  ./pack_logs.sh

Output:

  exports/mac_power_logs_all_YYYYMMDD_HHMMSS.tar.xz

CONFIGURATION
-------------

Application attribution options on mac_power_watch.py:

  --app-power                         Enable app attribution
  --no-app-power                      Disable app attribution
  --app-power-every 10                Seconds between app activity samples
  --app-power-sample-ms 1000          powermetrics sample window
  --app-power-top 3                   Top apps shown live
  --app-power-max-activities 160      Bounded records retained per sample
  --app-power-min-score 0.001         Minimum positive activity score
  --app-power-summary-every 3         Summary flush interval in app samples
  --no-app-power-resolve-bundles      Disable optional Spotlight name lookup

The default 10-second activity interval reduces measurement overhead. Total app
watts are recalculated on every monitor refresh using the newest available
application activity distribution.

ERROR HANDLING AND FALLBACKS
----------------------------

- sudo is refreshed by the powermetrics wrapper.
- powermetrics commands have bounded timeouts.
- unsupported advanced process flags fall back to simpler commands.
- malformed/unknown plist fields are ignored safely.
- if powermetrics app sampling fails, a low-confidence ps CPU/RAM fallback is
  used instead of stopping the main monitor.
- output sizes are bounded by maximum activity and top-app limits.
- JSON summaries are written atomically.
- JSONL is streamed incrementally to avoid retaining a full session in memory.

ACCURACY
--------

Per-application electrical watts are not directly exposed by macOS. The
estimates are intended for:

- ranking apps and services on the same Mac
- comparing app versions or workloads
- identifying likely battery drain causes
- correlating applications with benchmark phases
- comparing macOS builds and power modes

They should not be interpreted as laboratory-grade direct per-process power or
used for cross-device comparisons. A wall/USB-C meter is still required for
exact wall power.

ARCHITECTURE
------------

See:

  APP_POWER_ATTRIBUTION.md

Primary implementation:

  app_power_attribution.py
  generate_app_power_report.py
  mac_power_watch.py
  tests/test_app_power_attribution.py

TESTS
-----

  ./run_tests.sh

Or directly:

  python3 -m unittest discover -s tests -v

The suite validates plist parsing, current task fields, coalition aggregation,
aggregate-row exclusion, live watt recalculation, baseline confidence, power
allocation totals, and streaming summary output.

The complete historical development notes are preserved in:

  CHANGELOG_LEGACY.txt
