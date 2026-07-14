# MacPowerLab v1.1.0

## Benchmark experience

- Added a backend-owned benchmark catalog with user-facing descriptions,
  phases, durations, intensity, power requirements, safety notes, and metrics.
- Added presets:
  - Quick diagnostic
  - Idle baseline
  - CPU sustained load
  - GPU sustained load
  - Memory bandwidth
  - Mixed system load
  - Application power audit
  - Battery discharge suite
  - AC adapter / charging suite
  - Thermal stability
  - Extreme soak
- Added a custom benchmark builder for arbitrary CPU/GPU/memory combinations,
  GPU intensity, memory allocation, duration, baseline, cooldown, and required
  power source.
- Added `macpowerlab benchmark list`.
- Added custom CLI mode.

## SwiftUI

- Replaced the three-option segmented control with a full benchmark picker.
- Added explanations of what each benchmark does, who it is for, what it
  measures, its phases, estimated duration, power requirement, and safety notes.
- Added custom benchmark controls and total-duration estimation.
- Added detailed phase, elapsed, and remaining progress.
- Made every application-power table column sortable.
- Added CPU W, GPU W, and Energy Impact columns.
- Added application search and zero-use filtering.

## API

- Added authenticated `GET /benchmarks`.
- Extended `POST /benchmark/start` with a strictly validated custom payload.
