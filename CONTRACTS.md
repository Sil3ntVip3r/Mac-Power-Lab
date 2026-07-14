# Contracts

The canonical schema version is `v1`. Contract files live in `contracts/v1` and are mirrored to `schemas/` for compatibility.

- `macpowerlab.power_sample.v1`
- `macpowerlab.app_power.v1`
- `macpowerlab.event.v1`
- `macpowerlab.test_run.v1`
- `macpowerlab.session.v1`
- `macpowerlab.status.v1`
- `macpowerlab.session_summary.v1`
- `macpowerlab.parity_report.v1`

Changes that remove or reinterpret existing fields require a new schema version. Additive optional fields may be introduced within v1. JSONL is the canonical streaming format; reports and SQLite are derived views.
