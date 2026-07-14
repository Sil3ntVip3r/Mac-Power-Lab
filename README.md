# Mac Power Lab

[![CI](https://github.com/Sil3ntVip3r/Mac-Power-Lab/actions/workflows/ci.yml/badge.svg)](https://github.com/Sil3ntVip3r/Mac-Power-Lab/actions/workflows/ci.yml)
[![CodeQL](https://github.com/Sil3ntVip3r/Mac-Power-Lab/actions/workflows/codeql.yml/badge.svg)](https://github.com/Sil3ntVip3r/Mac-Power-Lab/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Sil3ntVip3r/Mac-Power-Lab)](go.mod)

Mac Power Lab is a power-first macOS monitoring, application-attribution, battery benchmarking, thermal-analysis, and charger-diagnostics platform.

It combines a highly concurrent **Go engine**, native CPU/memory/Metal workloads, and a **SwiftUI desktop application**. The project is designed for repeatable same-Mac comparisons across macOS versions, battery age, power modes, workloads, chargers, and application mixes.

> [!IMPORTANT]
> macOS does not expose direct electrical watts for every application. Per-app watt values are confidence-labelled attribution estimates calibrated against the best available total-system power signal.

## Features

- Live battery charge/discharge power, voltage, current, capacity, health, cell balance, and temperature
- Charger rating, USB-C PD contract, charge acceptance, estimated output, headroom, and battery assist
- CPU, GPU, ANE, DRAM, package power, Apple Silicon cluster frequency/residency, and macOS thermal pressure
- Per-application Energy Impact, CPU/GPU time, wakeups, disk/network activity, and estimated watts/Wh
- Battery, AC-adapter, CPU, GPU, memory, mixed-load, thermal, application-audit, and extreme-soak benchmarks
- Validated custom benchmark builder
- Canonical JSONL storage, optional SQLite mirror, HTML/Markdown reports, comparisons, and compressed archives
- Terminal UI, CLI, authenticated loopback API, and native SwiftUI dashboard
- Frozen legacy Python/zsh implementation for parity and rollback

## Requirements

- Apple Silicon or Intel Mac
- macOS 14 or later for the SwiftUI application
- Go 1.23 or later
- Xcode or Command Line Tools
- Metal Toolchain for GPU benchmarks

Install the Metal Toolchain when required:

```bash
xcodebuild -downloadComponent MetalToolchain
```

## Quick start

```bash
git clone https://github.com/Sil3ntVip3r/Mac-Power-Lab.git
cd Mac-Power-Lab
chmod +x *.sh scripts/*.sh
./scripts/bootstrap_macos.sh
open ./dist/MacPowerLab.app
```

The bootstrap removes quarantine only from this project directory, builds and ad-hoc signs local executables, runs tests, builds native workloads, and creates the app bundle. It does **not** disable Gatekeeper globally.

## CLI examples

```bash
# Live terminal monitor
./bin/macpowerlab monitor

# Low-overhead diagnostic monitor
./bin/macpowerlab monitor --safe

# List benchmark presets
./bin/macpowerlab benchmark list

# Full unplugged battery suite
./bin/macpowerlab benchmark battery

# AC adapter and charging suite
./bin/macpowerlab benchmark ac

# 30-minute maximum-load soak
./bin/macpowerlab benchmark extreme --duration 30m

# Application power audit
./bin/macpowerlab benchmark app-audit --duration 20m

# Custom CPU + GPU battery test
./bin/macpowerlab benchmark custom \
  --name "CPU and GPU battery test" \
  --cpu --gpu \
  --gpu-profile high \
  --duration 10m \
  --baseline 2m \
  --cooldown 3m \
  --power-source battery
```

## Architecture

```text
SwiftUI app / CLI / TUI
          │
          ▼
Authenticated loopback API and Go engine
          │
          ├── battery, charger, PowerTelemetry, PowerDistribution
          ├── powermetrics CPU/GPU/thermal and process coalitions
          ├── application power attribution
          ├── benchmark controller and native workloads
          ├── JSONL / optional SQLite storage
          └── reports, comparisons, and archives
```

See:

- [Architecture](ARCHITECTURE.md)
- [Build on macOS](BUILD_ON_MAC.md)
- [Data contracts](CONTRACTS.md)
- [Security](SECURITY.md)
- [Validation](VALIDATION.md)
- [Migration history](MIGRATION_PHASES.md)
- [v1.4.0 engineering review](docs/engineering/v1.4.0.md)
- [Release notes](docs/releases/)

## Accuracy and interpretation

Mac Power Lab is intended for:

- repeatable comparisons on the same Mac
- finding regressions across macOS builds
- identifying likely application power consumers
- evaluating battery and charger behaviour under controlled workloads
- observing thermal pressure and performance stability

It is not a calibrated laboratory power meter. Exact wall power requires an external USB-C or wall power meter, and cross-device comparisons require controlled methodology.

## Development

```bash
make test
make vet
make build

# Full race test
GOMAXPROCS=2 go test -race -p 1 ./...

# Build and validate on macOS
./scripts/validate_on_mac.sh
```

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Security issues should be reported according to [SECURITY.md](SECURITY.md), not in a public issue.

## License

Mac Power Lab is available under the [MIT License](LICENSE).
