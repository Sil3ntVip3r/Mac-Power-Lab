# MacPowerLab Engineering Instructions

## Before editing

1. Read this file and `CONTRIBUTING.md`.
2. Inspect the task, acceptance criteria, Git status, current branch, recent
   commits, exact diff, relevant callers/callees, public contracts, tests,
   configuration, and documentation.
3. State compatibility and safety risks before making broad changes.

## Engineering rules

- Prefer the smallest correct change and consumer-owned Go interfaces.
- Preserve existing public behavior and v1 contracts unless a change is
  explicitly additive or versioned.
- Propagate `context.Context` through blocking work and make goroutine ownership
  and shutdown explicit.
- Bound subprocess output, queues, maps, parser depth, history, and retention.
- Preserve physical units explicitly; never infer units from magnitude.
- Do not add production dependencies without approval.
- Never add network telemetry or uploads without an approved privacy design.
- Local-only storage and operation remain the default.
- Never commit tokens, credentials, usernames, hostnames, serial numbers, raw
  user logs, personal sensor captures, generated binaries, `.app` bundles, or
  local databases.
- Do not stage, commit, push, merge, publish, or deploy unless explicitly asked.

## Required validation

For Go or concurrency changes, run and report:

```bash
test -z "$(gofmt -l cmd internal)"
go test -count=1 ./...
go vet ./...
go test -race ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./cmd/macpowerlab
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./cmd/macpowerlab
```

For SwiftUI changes, parse the Swift sources and perform the native macOS build
when running on a Mac. Never claim a check passed unless it was run and its
result was observed. Clearly list skipped real-Mac or hardware checks.
