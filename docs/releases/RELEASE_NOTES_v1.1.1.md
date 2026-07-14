# MacPowerLab v1.1.1

## SwiftUI build hotfix

v1.1.0 could fail while compiling the SwiftUI frontend with:

```text
value of type 'String' has no member 'localizedStandardLowercased'
```

The application bundle was created before compilation, so the failed build left
an incomplete `dist/MacPowerLab.app`. Attempting to open it then produced:

```text
The application cannot be opened because its executable is missing.
```

The zsh error trap also referenced `$ZSH_DEBUG_CMD` under `set -u`, producing a
second misleading error:

```text
ZSH_DEBUG_CMD: parameter not set
```

## Fixes

- Replaced the unavailable sorting property with a normalized,
  case-insensitive and diacritic-insensitive sort key.
- Made every zsh error trap safe when `ZSH_DEBUG_CMD` is unset.
- The SwiftUI build now removes an incomplete app bundle automatically on error.
- The app executable is checked immediately after Swift compilation.
- Bootstrap removes stale partial app bundles before rebuilding.
- Updated app and backend versions to 1.1.1.
