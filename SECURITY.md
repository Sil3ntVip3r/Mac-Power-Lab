# Security

- Privileged collection uses `/usr/bin/sudo -n` after an explicit interactive `sudo -v` performed in a visible terminal.
- The engine never stores a password.
- Commands are launched with `os/exec` and no implicit shell expansion.
- Subprocess output is bounded and commands use context cancellation/timeouts.
- Session and token files are created with user-only permissions.
- The API refuses non-loopback addresses and requires a random bearer token for all non-health routes.
- Request bodies are size-limited and reject unknown or multiple JSON values.
- Archives include SHA-256 hashes and normalized metadata.
- App power values are labelled estimates; they are not represented as direct metering.
