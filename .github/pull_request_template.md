## Summary

## Why

## Validation

- [ ] `gofmt -w` produced no remaining diff
- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] Race tests were run for concurrency-sensitive changes
- [ ] macOS live validation was run when sensor, benchmark, Metal, signing, or SwiftUI behavior changed

## Compatibility and safety

- [ ] Public schemas and CLI/API behavior remain compatible, or the change is explicitly versioned
- [ ] No secrets, tokens, personal logs, generated binaries, or private paths are included
- [ ] Documentation and `CHANGELOG.md` are updated when behavior changes
