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
  Shared flag groups: `filter_flags.go` (`--grep`/`--where`/`--fields`/`--output`/
  `--stats`, and `buildPipelineOptions`), `output_flags.go` (`--color`/`--theme`),
  `log_query.go` (`--since`/`--tail`/`--timestamps`), and `config.go` (the optional
  `~/.config/logx/config.yaml`; flag > config > built-in default).
- `internal/logging/` — the parsing/formatting/filtering engine (format-agnostic;
  no Kubernetes dependency). `parser.go` (+ `parser_formats.go` for XML/CSV/YAML)
  classifies a line; `LevelTracker` (`continuation.go`) groups multi-line entries;
  `pipeline.go` is the single shared parse→group→filter→render engine both
  `logx logs` and `logx parse` drive (`filter.go`'s `FilterAndFormatLogs` is now a
  thin wrapper over it). `predicate.go` evaluates `--where`; `format.go` + `theme.go`
  render (`--color`/`--theme`); `output_json.go` does `--output json`; `stats.go`
  powers `--stats`; `aliases.go` registers custom field names from config.
- `internal/kubernetes/` — client construction and the `LogFetcher` (logs +
  timeline). `multistream.go` fans in `--all-containers`/`--selector`/
  `--all-namespaces` through a bounded worker pool (`--max-concurrency`, default
  10) with serialized, color-prefixed writes; `--stats` across multiple streams
  records into one thread-safe `logging.Stats`. Behind `kubernetes.Interface` for
  testability.
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

- **Levels span `TRACE < DEBUG < INFO < WARN < ERROR < FATAL`.** `TRACE` is below
  the default `DEBUG`, so trace lines are opt-in (`-l TRACE`). `FATAL` comes from
  klog `F`, textual `FATAL`/`PANIC`, and numeric `60` (bunyan/pino).
- **Level mapping is intentional**: numeric `0` maps to INFO (zap convention, not
  syslog), numeric `10` maps to TRACE (bunyan/pino), and a successful HTTP status
  (`2xx`/`3xx`) maps to DEBUG so high-volume access logs stay out of the default
  INFO view. All are pinned by tests.
- **An undetected level must default to DEBUG, never TRACE.** TRACE is below the
  default filter, so defaulting there hides the line. Every parser sets
  `Level: DEBUG` explicitly — the zero value of `LogLevel` is TRACE, and relying on
  it is how the XML parser once made every level-less XML line invisible.
  Relatedly, plain-text level detection is **positional**, via two regexes.
  `plainTextLeadingLevelRegex` trusts a level at the head of the line (optionally
  after a timestamp and/or brackets) — every level, TRACE included.
  `plainTextLevelRegex` then scans the rest of the line and deliberately omits
  TRACE, because a non-leading "trace" is overwhelmingly prose ("stack trace
  follows", "STACK TRACE follows", "Blocked TRACE request", "TRACE-ID:"). Casing is
  not a sufficient guard — the first attempt required uppercase and still lost
  uppercase prose. Excluding TRACE from the fallback also lets a real level later
  on the line win instead of losing to a leftmost prose "TRACE".
- **A plain-text timestamp must come from the head of the line.** The extraction
  regex is anchored for the same reason: an unanchored search made a date mentioned
  in the message ("Scheduled next backup for 2026-12-25") the entry's timestamp,
  corrupting `--timeline` ordering, `ts` predicates, and the `.time` field of JSON
  output. `stripLeadingMeta` strips level and timestamp in either order, since
  loggers disagree on which comes first.
- **A false logfmt match is a level-loss bug.** logfmt is tried before
  klog/syslog/access/xml/csv, so any line it wrongly claims never reaches the
  parser that would have read it correctly. `parseLogfmtFields` therefore validates
  keys with `logfmtKeyRegex`, the same guard `splitTrailingFields` uses.
- **Everything printed goes through `terminal.Sanitize` — including `--output
  json`.** A single chokepoint only works if every render path actually uses it.
  Two paths missed it: `--stats` embedded untrusted content in its "top messages"
  section, and NDJSON relied on `json.Marshal`, which escapes only code points
  below `0x20` — DEL, the C1 controls (a UTF-8 terminal reads `U+009B` as CSI), and
  Trojan-Source bidi overrides all passed through raw. **Valid JSON is not safe
  JSON**; NDJSON is read in a terminal as often as it is piped to `jq`. Note also
  that the digest prints only the top 5 messages, so an adversarial-corpus test can
  pass while a hostile line is recorded but sorted out of view — assert against a
  controlled digest, not only the corpus.
- **The parser is the single message resolver.** `entry.Message` is authoritative;
  the formatter must not re-derive it from `Fields`. It once did, and the two
  copies disagreed — `{"message":"","msg":"real"}` rendered blank in text while
  JSON rendered correctly. Message aliases are resolved by first *non-empty* value,
  since enrichers (Fluent Bit, Vector) add a normalized key alongside the original.
- **A JSON entry must never be silently emptied.** `MarshalEntryJSON` always emits
  the message (blanking it when it equalled `RawLine` erased the content of every
  unstructured line, since `jsonEntry` has no `raw` field), `sanitizeJSONValue`
  converts NaN/±Inf to their textual form (they made `Marshal` fail, and the error
  was swallowed into a bare `"{}"`), and a projection that resolves no requested
  key skips the entry instead of emitting `{}` or a blank line.
- **Field-name matching is case-insensitive throughout.** Exact match wins; the
  fold fallback runs only on a miss, and ties resolve to the lexicographically
  smallest key so map iteration order cannot make it flap. This covers Serilog and
  .NET (`Level`, `Msg`, `Timestamp`), whose ERROR lines were previously invisible
  at `--level ERROR`. The display-exclusion sets fold too, or the message prints
  twice. `hasAnyField` deliberately uses `fieldValueExact`: the 16-name HTTP-shape
  heuristic is not worth the scan (see `BenchmarkParseJSONWideFields`, which
  documents the measured cost).
- **Match highlighting runs on already-colorized text**, so `highlightMatches`
  strips the theme's escape sequences, matches the visible text, and maps offsets
  back. Matching the rendered string directly let ordinary patterns (`[0-9]+`,
  `[a-z]`, even `m`) match inside an SGR sequence and corrupt it. The invariant,
  fuzzed by `FuzzHighlightPreservesVisibleText`, is that highlighting only inserts
  reverse-video toggles and never changes the visible text.
- **`--where` compares by type rather than coercing.** Equality tries exact string
  first so numeric-looking identifiers do not collide (zero-padded IDs, and
  19-digit span IDs that float64 cannot tell apart); `ts` comparisons are
  chronological, not string-based; and a field literally present in the line wins
  over a colliding virtual key, so a log carrying its own `source` field is
  reachable instead of resolving to the guessed logger label.
- **Custom field names** (config `fields:`) feed both JSON and logfmt parsing via
  `logging.RegisterFieldAliases`, which is additive (built-in names keep priority)
  and rebuilds the formatted-field exclusion set so a custom level/timestamp key is
  not also printed as an ordinary field. Built-in logfmt-only keys (e.g. `lvl`) and
  the logfmt logger keys stay fixed.
- **`--stats` aggregates across streams**: with `--all-containers`/`--selector`
  every concurrent stream records into one shared, mutex-guarded `logging.Stats`
  and a single digest is written after all streams finish (per-line output is
  suppressed in stats mode, so nothing interleaves before it). `--stats` is still
  rejected with `--timeline`. The digest is written even when a stream fails:
  streams are independent by design, so the aggregate over the ones that succeeded
  is still the useful answer.
- **Multi-line grouping** (stack traces): an *indented* continuation line inherits
  the level of the entry it belongs to, so a stack trace stays visible at its
  parent's `--level`. The level tracker carries the parent across intervening
  flush-left lines (required so a Java/Go/Python flush-left exception/panic header
  doesn't orphan its indented frames). The tradeoff is a bias toward
  over-inclusion; a perfectly precise version needs one-line lookahead, which is
  incompatible with `--follow` streaming. See `internal/logging/continuation.go`.
- `--timeline` shows only the target pod's own events (server-side field selector
  plus a client-side guard on **both** name and kind, since a Service or Deployment
  sharing the pod's name is common) and cannot be combined with `--follow`.
  `--since`/`--tail` bound the log portion of the timeline (events stay bounded
  separately by `maxTimelineEvents`). Content filters
  (`--grep`/`--exclude`/`--where`) do apply to its log portion, through the shared
  `PipelineOptions.MatchesContent`; the flags that would replace its fixed
  two-record-type rendering (`--fields`, `--output json`, `--stats`) are rejected
  rather than silently ignored.
- **`--previous` requires a single target** and is rejected with `--all-containers`
  or `--selector`: those paths never run the `-p` precondition check, so every
  stream was stamped `Previous: true` and any container that had not restarted
  failed the whole command. Its precondition check scans init and ephemeral
  container statuses too, matching `podHasContainer`.
- **`--follow` must open every stream.** `effectiveMaxConcurrency` ignores
  `--max-concurrency` when `Follow` is set: errgroup's limit blocks the dispatch
  loop until a slot frees, and a followed stream never returns, so a pool smaller
  than the stream count does not throttle — it *starves*. Streams past the limit
  were never opened at all, so `logx logs -f --selector app=api` on a 20-replica
  Deployment silently tailed 10 pods forever. The cap still applies to a finite
  fetch, where bounding the burst is exactly right.
- **Long-running loops must observe `cmd.Context()`.** `signal.NotifyContext`
  takes SIGINT/SIGTERM away from the process's default disposition, so any loop
  that ignores the context becomes *uninterruptible* rather than merely
  ungraceful — `logx parse` on a large file could not be stopped short of
  SIGKILL. `Execute` also re-arms the default disposition on the first signal so a
  second Ctrl-C always terminates, and it suppresses only errors that genuinely
  *are* the cancellation (`errors.Is(err, context.Canceled)`); the earlier check
  tested only `ctx.Err() != nil`, so after a Ctrl-C any real failure was reported
  as exit 0.
- **Package-level parser/theme globals are startup-only, by convention not
  enforcement.** `RegisterFieldAliases` and `ApplyColorMode`/`SetTheme` mutate
  globals that every parse and render reads; `go test -race` flags the write/read
  pair if they are ever called concurrently. It is safe today only because both
  run from `PersistentPreRunE` before any stream goroutine exists. Anything that
  moves them later (a config reload, a per-subcommand alias flag) needs an
  `atomic.Pointer` or `sync.Once` first — these are slice-header writes, so a torn
  read is a crash, not just a stale field list.
- **Metamorphic tests guard the engine's invariants** (`metamorphic_test.go`):
  lowering `--level` can only add lines; `--grep P` and `--exclude P` partition
  the input exactly; text and JSON keep the same lines and report the same level;
  `--stats` counts what the same filters emit; output is deterministic across
  runs despite randomized map iteration. Each has a fuzz counterpart. These catch
  cross-mode inconsistencies that example-based tests structurally cannot.
- **Shell completion** for value-enum flags (`--level`/`--theme`/`--color`/
  `--output`) and field-name hints (`--fields`/`--where`) is registered in
  `cmd/completion.go` and wired from the flag-group helpers, so both `logs` and
  `parse` get it.
