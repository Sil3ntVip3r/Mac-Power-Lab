# Contracts

The canonical schema version is `v1`. Contract files live in `contracts/v1` and are mirrored to `schemas/` for compatibility.

- `macpowerlab.power_sample.v1`
- `macpowerlab.app_power.v1`
- `macpowerlab.event.v1`
- `macpowerlab.test_run.v1`
- `macpowerlab.session.v1`
- `macpowerlab.status.v1`
- `macpowerlab.session_summary.v1`
- `macpowerlab.report_artifact.v1`
- `macpowerlab.parity_report.v1`
- `macpowerlab.runtime_settings.v1`

Changes that remove or reinterpret existing fields require a new schema version. Additive optional fields may be introduced within v1. JSONL is the canonical streaming format; reports and SQLite are derived views.

`macpowerlab.session.v1` now permits the additive `runtime_settings` property. New monitoring sessions always include it; readers of older session documents must continue to accept its absence. The standalone settings schema is strict and rejects unknown fields, invalid profile identifiers, out-of-range values, and inconsistent logging state.

## Timestamped cumulative reports

Each report request captures flushed JSONL byte boundaries and generates an immutable artifact under:

```text
sessions/<session-id>/reports/<timestamp>/
  MacPowerLab_Summary_<timestamp>.json
  MacPowerLab_Report_<timestamp>.md
  MacPowerLab_Report_<timestamp>.html
  artifact.json
```

`reports/latest.json` points to the newest completed artifact. Historical report directories are never overwritten. Each artifact contains all session records from the session start through its `data_through` boundary.
