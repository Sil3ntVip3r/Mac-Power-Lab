# MacPowerLab v1.2.0

## Monitor workspaces

The original dashboard combined summary cards and a large application table in
one vertically scrolling view. This limited the amount of sensor information
that could be displayed and created nested scrolling/layout problems in the
application table.

v1.2.0 introduces dedicated monitor workspaces:

- **Overview** — essential live power, battery, thermal, performance, and top-app
  summary.
- **Battery & Charging** — electrical flow, capacities, health, cell balance,
  runtime estimates, USB-C contract, adapter output, headroom, and battery assist.
- **Performance** — CPU/GPU/ANE/DRAM power, package power, frequencies, CPU
  clusters, thermal signals, and collector health.
- **Applications** — dedicated full-height sortable attribution table.
- **Full Monitor** — every available live statistic grouped by subsystem,
  intentionally without application rows.

## Application table

- Removed the application table from the overview's outer ScrollView.
- Dedicated full-height layout prevents the first sorted row from being clipped.
- Added table modes:
  - Power
  - Activity
  - I/O
  - Identity
- Added sortable residual watts, energy share, CPU/GPU time, wakeups, disk,
  network, PID, responsible PID, coalition, and attribution source.
- Added filtering and zero-use hiding without disturbing the table viewport.

## Data surfaced

The SwiftUI contracts now expose the full Go monitoring model, including:

- Battery capacities, estimated Wh, cell min/max/delta, QMax/Ra deltas, and
  time-to-empty/full.
- BMS SystemPower, effective total load, and PowerDistribution input.
- Adapter PD contract, battery assist, headroom, and port-controller maximum.
- Cluster power and CPU frequency estimate source.
- Thermal source.
- Attribution component pools.
- Sample sequence, learned baseline, collector status, complete session metadata,
  capabilities, benchmark status, warnings, and engine errors.
