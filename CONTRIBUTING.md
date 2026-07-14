# Contributing to Mac Power Lab

Thank you for contributing.

## Development setup

```bash
git clone https://github.com/Sil3ntVip3r/Mac-Power-Lab.git
cd Mac-Power-Lab
make test
make vet
```

macOS-specific validation:

```bash
chmod +x *.sh scripts/*.sh
./scripts/validate_on_mac.sh
```

## Pull requests

1. Create a focused branch from `main`.
2. Add or update tests for every behaviour change.
3. Run `gofmt`, `go test ./...`, `go vet ./...`, and the race detector for concurrent changes.
4. Keep public contracts backward compatible unless the change is explicitly versioned.
5. Update documentation and `CHANGELOG.md` when behaviour changes.
6. Do not commit binaries, build products, logs, tokens, local database files, or personal sensor captures.

## Go engineering expectations

- Prefer small consumer-owned interfaces.
- Propagate `context.Context` through blocking operations.
- Make goroutine ownership and shutdown explicit.
- Avoid silent error swallowing; optional subsystem degradation must be visible.
- Bound subprocess output, queues, maps, and parser depth.
- Preserve physical units explicitly rather than inferring them by magnitude.
- Use `errors.Is`, `errors.As`, `%w`, and `errors.Join` where appropriate.

## Commit messages

Use concise conventional-style messages where practical:

```text
feat: add battery comparison view
fix: prevent stale process attribution
refactor: isolate monitor lifecycle ownership
test: cover benchmark cancellation
```

## Security

Do not report vulnerabilities publicly. Follow [SECURITY.md](SECURITY.md).
