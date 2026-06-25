# CLAUDE.md

Guidance for AI agents (and humans) working in this repository.

## What this is

`logx` is an enhanced Kubernetes pod log viewer and `kubectl` plugin. It fetches
pod logs and events, parses common structured and plain-text log formats, detects
levels and timestamps, and prints colorized, filterable output. It ships as two
binaries: `logx` (standalone) and `kubectl-logx` (the plugin form).

## Conventions (read before committing)

- **Do NOT add any `Co-Authored-By` / co-authoring trailer to commit messages —
  ever.** This is a hard project rule.
- Use Conventional Commit style subjects (`feat:`, `fix:`, `refactor:`, `test:`,
  `chore:`, `docs:`, with optional scope, e.g. `fix(timeline): ...`).
- Keep the tree green before committing: `go build ./...`, `go vet ./...`,
  `gofmt -l .` (empty), `go test ./...`, and `golangci-lint run ./...` (0 issues).
- New behavior gets a test. Prefer table-driven tests and the real client-go fake
  (`fake.NewSimpleClientset`) over hand-rolled mocks.

## Layout

- `main.go` — thin entry point; calls `cmd.Execute()` and owns the single stderr
  write + exit code.
- `cmd/` — Cobra commands. All use `RunE` and return errors (never `os.Exit` in a
  command). Flag names are constants in `cmd/flags.go`. A swappable
  `newKubernetesClient` seam in `cmd/kube_flags.go` lets tests inject a fake.
- `internal/logging/` — the parsing/formatting/filtering engine (format-agnostic;
  no Kubernetes dependency). `parser.go` classifies a line; `LevelTracker`
  (`continuation.go`) groups multi-line entries; `filter.go` is the reader→writer
  pipeline.
- `internal/kubernetes/` — client construction and the `LogFetcher` (logs +
  timeline). Behind `kubernetes.Interface` for testability.
- `internal/format/` — output formatting (table/json/yaml/posix) via tagged DTOs.
- `internal/terminal/` — `Sanitize`, the single chokepoint for neutralizing
  control bytes and Unicode-spoofing characters in untrusted output.
- `internal/version/` — version info set via ldflags.

`internal/` is deliberate: nothing here is a public library API.

## Common commands

```bash
just build            # build both binaries with version ldflags into bin/
just test             # go test ./...
just lint             # golangci-lint run
go test -race ./...   # race detector
$(go env GOPATH)/bin/govulncheck ./...   # vuln scan (also in CI)
```

## Testing the parser without a cluster

The tool never needs a real cluster to exercise its core value (parsing,
leveling, filtering, formatting). Two ways to test against arbitrary logs:

- `logx parse [file]` (or pipe to stdin: `cat app.log | logx parse -l WARN`)
  runs a log source through the same parsing/formatting pipeline as `logx logs`,
  including multi-line grouping. This is the easiest way to eyeball output for a
  sample file.
- In Go tests, feed synthetic log content through `LogFetcher.GetLogs` using a
  `fake.NewSimpleClientset` with a `pods/log` reactor (see
  `internal/kubernetes/logs_test.go`).

Sample log files covering the formats logx is expected to handle live in
`internal/kubernetes/testdata/` and `internal/logging/testdata/`.

## Behavioral notes / known limitations

- **Level mapping is intentional**: numeric `0` maps to INFO (zap convention, not
  syslog), and a successful HTTP status (`2xx`/`3xx`) maps to DEBUG so high-volume
  access logs stay out of the default INFO view. Both are pinned by tests.
- **Multi-line grouping** (stack traces): an *indented* continuation line inherits
  the level of the entry it belongs to, so a stack trace stays visible at its
  parent's `--level`. The level tracker carries the parent across intervening
  flush-left lines (required so a Java/Go/Python flush-left exception/panic header
  doesn't orphan its indented frames). The tradeoff is a bias toward
  over-inclusion; a perfectly precise version needs one-line lookahead, which is
  incompatible with `--follow` streaming. See `internal/logging/continuation.go`.
- `--timeline` shows only the target pod's own events (server-side field selector
  plus a client-side guard) and cannot be combined with `--follow`.
