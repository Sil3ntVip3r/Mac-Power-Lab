# Build MacPowerLab v1.4.0 on macOS

## Recommended

```bash
chmod +x *.sh scripts/*.sh
./scripts/bootstrap_macos.sh
```

The bootstrap clears quarantine only from this project, builds and ad-hoc signs
the Go CLI, validates it, builds native workloads, and creates the SwiftUI app.

## Manual

```bash
./scripts/prepare_macos_security.sh
./scripts/build_macos.sh
./scripts/run_tests.sh
./scripts/build_native.sh
./scripts/build_swiftui_app.sh
```

## Install

```bash
./scripts/install_app.sh
```

## Diagnostics

```bash
./scripts/diagnose_macos_security.sh
```
