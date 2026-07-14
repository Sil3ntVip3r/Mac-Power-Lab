# MacPowerLab Sensor Roadmap

MacPowerLab remains power-first, but v0.6.0 starts turning it into a broader system monitor.

## Source-guided sensor work

Apple Open Source releases can help us inspect public/open parts of macOS, IOKit,
power management, and related tools where Apple publishes source. It will not
expose every private SMC, AppleSmartBattery, or Apple Silicon hardware detail.

Use it as a source map, not as a guarantee that every internal sensor is public.

## Sensor categories

### Power — primary focus
- Battery charge/discharge watts
- BMS SystemPower
- Adapter rating, voltage, current, calculated watts
- Charger load/headroom
- Port-controller max power and warnings
- PowerTelemetry debug fields
- Wall/USB-C meter manual readings
- Energy charged/discharged Wh
- Charge curve by battery percentage band

### Thermal
- Battery physical temperature
- Battery thermal trend
- powermetrics thermal pressure
- powermetrics SMC/thermal sensor lines when exposed
- future parser for CPU/GPU die/package temperatures if macOS exposes them

### Cooling
- powermetrics SMC fan lines when exposed
- fan RPM if available
- fan/thermal correlation under load

### Performance
- powermetrics CPU/GPU/ANE/DRAM estimates
- CPU/GPU frequencies when exposed
- current phase performance/power stats
- thermal or power throttling indicators

### System health
- Battery condition and cycle count
- Cell voltage delta
- Qmax / WeightedRa spread
- Cell disconnect count
- Memory pressure snapshot
- Top CPU processes
- Disk free space snapshot
- OS/build/hardware inventory

## v0.6.0 new tools

- Friendly progress UI for test runners
- Interactive test menu
- System sensor snapshot/report
- Raw powermetrics sensor capture for parser improvement

## v0.9.0 application power attribution

- `powermetrics` task and resource-coalition Energy Impact
- responsible-PID helper grouping when coalition data is unavailable
- CPU and GPU component-aware watt allocation
- dynamic watts above quiet baseline
- interrupt and package-idle wakeups
- disk and network activity
- per-app estimated Wh
- benchmark-window application reports
- confidence/source/fallback tracking
