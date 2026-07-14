# MacPowerLab v1.0.0

This release completes the six-phase migration from a script-coordinated prototype to a production-oriented macOS platform.

## Highlights

- Dependency-free Go monitoring and benchmark engine.
- Versioned JSON contracts and compatibility fixtures.
- Live AppleSmartBattery and `powermetrics` collectors.
- Resource-coalition application power attribution.
- Streaming JSONL plus optional SQLite persistence.
- Battery, AC, and extreme benchmark state machines.
- Fixed-screen Go terminal dashboard.
- Markdown/HTML reporting, comparisons, and maximum-compression archives.
- Live parity against the frozen v0.9.0 implementation.
- Token-authenticated loopback API.
- Native SwiftUI dashboard and benchmark controls in source, with a macOS app-bundle build/signing pipeline.
- Darwin arm64 and amd64 binaries included.

## Compatibility

The legacy implementation remains under `legacy/` and can be invoked with:

```bash
./bin/macpowerlab legacy run_power_monitor_powermetrics_awake.sh
```

## Attribution accuracy

Application watt values are estimates calibrated to total system power. MacPowerLab stores the underlying activity metrics, attribution method, and confidence so reports remain auditable.
