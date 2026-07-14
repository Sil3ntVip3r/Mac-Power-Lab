# MacPowerLab v1.0.2

## Root cause fixed

The downloaded source archive can carry `com.apple.quarantine`. On the affected
Mac, the locally built `bin/macpowerlab` inherited a security state that caused
macOS to terminate it with SIGKILL before the Go program entered `main()`.

This produced three connected symptoms:

- `zsh: killed ./bin/macpowerlab monitor`
- `build_native.sh` exited before creating native workloads
- `build_swiftui_app.sh` exited before creating `dist/MacPowerLab.app`

## Changes

- Added `scripts/prepare_macos_security.sh`.
- Added one-command `scripts/bootstrap_macos.sh`.
- `build_macos.sh` now removes project-scoped quarantine, ad-hoc signs all Darwin
  binaries, verifies signatures, and runs a real CLI smoke test.
- `build_native.sh` signs and verifies every workload executable.
- `build_swiftui_app.sh` has visible build stages, clears quarantine, signs nested
  executables, validates Info.plist, and verifies the app bundle.
- Added `scripts/diagnose_macos_security.sh` and `scripts/install_app.sh`.

## Recommended

```bash
cd ~/Downloads/MacPowerLab_v1.0.2
chmod +x *.sh scripts/*.sh
./scripts/bootstrap_macos.sh
open ./dist/MacPowerLab.app
```

The project does not disable Gatekeeper globally. Quarantine removal is scoped
to the local MacPowerLab project and its locally built outputs.
