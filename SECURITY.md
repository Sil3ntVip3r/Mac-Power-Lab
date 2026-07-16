# Security

- Privileged collection uses `/usr/bin/sudo -n` after an explicit interactive `sudo -v` performed in a visible terminal.
- The engine never stores a password.
- Commands are launched with `os/exec` and no implicit shell expansion.
- Subprocess output is bounded and commands use context cancellation/timeouts.
- Session and token files are created with user-only permissions.
- Runtime settings are strictly decoded and atomically replaced as a private `0600` file inside the private `0700` data directory; settings symlinks and non-regular files are rejected on load.
- The API refuses non-loopback addresses and requires a random bearer token for all non-health routes.
- Request bodies are size-limited and reject unknown or multiple JSON values.
- Archives include SHA-256 hashes and normalized metadata. Archive collection excludes local API tokens, token-address files, launcher commands, SQLite sidecars, Finder metadata, and private-key material before any excluded file is opened.
- App power values are labelled estimates; they are not represented as direct metering.
- Process priority uses only the ordinary macOS nice range `-5...+10`. Benchmark children are normalized to nice `0`; MacPowerLab never requests kernel real-time scheduling.


## Support bundles

Use `macpowerlab support pack` rather than manually compressing the Application Support directory. The command writes outside the data directory and applies the archive deny policy. Excluded paths and reasons are recorded in `MANIFEST_macpowerlab.json`; excluded file contents are never read or written to the bundle.
