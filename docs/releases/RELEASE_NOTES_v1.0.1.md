# MacPowerLab v1.0.1

## Fixed

- Removed `NSAppleScript` Terminal automation from the SwiftUI backend launcher.
- Opens a private executable `.command` through Launch Services instead.
- Reuses an already-running backend instead of launching duplicates.
- Split continuous system-power sampling from process/application sampling.
- Continuous sampler now uses only `cpu_power,gpu_power,thermal`.
- Process Energy Impact and coalition data are collected every 10 seconds with a
  bounded one-shot plist capture.
- Process collection failures fall back to `ps` without stopping battery logging.
- Removed retention of full raw powermetrics dictionaries from live snapshots.
- Capped retained process rows at 512.
- Added `monitor --safe`.
- Prebuilds native workloads into the SwiftUI app bundle.
- Prevents recompilation from modifying a signed app bundle.
