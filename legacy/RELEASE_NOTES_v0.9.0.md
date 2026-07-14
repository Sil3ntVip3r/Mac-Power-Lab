# MacPowerLab v0.9.0 — Application Power Attribution

## Architecture

1. A dedicated `powermetrics` task/coalition sampler gathers Apple Energy Impact, CPU/GPU time, wakeups, disk I/O, and network activity.
2. A bounded background worker isolates app sampling from the primary battery/charger monitor.
3. A component-aware model distributes measured/estimated Mac power across apps using CPU time, GPU time, and Energy Impact.
4. A quiet-load baseline separates estimated dynamic app power from background/base system power.
5. Streaming JSONL plus atomic JSON/CSV summaries make long captures recoverable and scalable.
6. Markdown/HTML reports rank foreground apps, background services, system services, and benchmark workloads for the full run and each benchmark window.

## New outputs

- `logs/mac_power_<run>_apps.jsonl`
- `logs/mac_power_<run>_apps_summary.json`
- `logs/mac_power_<run>_apps_summary.csv`
- `logs/mac_power_<run>_apps_report.md`
- `logs/mac_power_<run>_apps_report.html`

## Accuracy

macOS does not expose direct electrical watts for each process. MacPowerLab reports confidence-labelled estimates calibrated to its total system-power signal. `estimated_dynamic_w` is the most useful field for comparing incremental app cost on the same Mac.
