# Release Notes

## Changed

- **Breaking:** `logx containers -o json` and `-o yaml` now emit lowercase,
  conventional keys (`pod`, `namespace`, `containers[].name/ready/status/image`)
  instead of the previous Go field names (`PodName`, `Name`, ...). Update any
  scripts that parse this output.
- `--timeline` now shows only events for the target pod (via a server-side field
  selector with a client-side guard) instead of every event in the namespace.
  This avoids leaking unrelated workloads, needs narrower RBAC, and de-noises the
  output. Repeated events are annotated with their occurrence count (e.g. `(x3)`).
- `SIGINT`/`SIGTERM` now cancel in-flight operations (notably a `--follow`
  stream) cleanly via a signal-aware context, instead of relying on abrupt
  process termination.
- CLI errors are reported once on stderr with a non-zero exit code; commands use
  Cobra's `RunE` so failures propagate consistently.

## Added

- Extended the level spectrum to `TRACE < DEBUG < INFO < WARN < ERROR < FATAL`.
  `TRACE` is below the default `DEBUG` (opt-in via `-l TRACE`); `FATAL` comes from
  klog `F`, textual `FATAL`/`PANIC`, and numeric `60` (bunyan/pino). Numeric `10`
  now maps to `TRACE`. The pinned mappings (numeric `0`→INFO, `2xx`/`3xx`→DEBUG)
  are unchanged.
- Content filtering shared by `logx logs` and `logx parse`: `--grep`/`--exclude`
  regexes (repeatable; matches are reverse-video highlighted) and `--where` field
  predicates (`==`, `!=`, `>`, `>=`, `<`, `<=`, `~=`) over structured fields and
  virtual keys (`level`/`message`/`logger`/`ts`), with severity-aware `level>=WARN`.
- `--fields ts,level,msg` projects output to chosen keys, and `--output json`
  emits normalized NDJSON (terminal-safe, de-duplicated fields) for `jq`.
- `--stats` prints a digest instead of the lines: counts by level, HTTP
  status class, and the top recurring (templated) messages.
- Server-side log windowing: `--since` (duration or RFC3339), `--tail`, and
  `--timestamps`.
- Multi-stream tailing with color-coded, non-interleaved prefixes:
  `--all-containers` (one pod), `--selector` (label-matched pods), and
  `--all-namespaces` (cluster-wide, with `--selector`).
- `--color`/`--no-color` (and `NO_COLOR`/TTY detection) plus a `--theme`
  (`dark`/`light`) for output appearance.
- `--timeline` now adds `[TERM]` entries with container termination details —
  exit code, signal, and reason such as `OOMKilled` — for the current and
  previous container instances.
- Recognize XML, CSV, and flow-style YAML single-line log bodies that previously
  fell through to plain text, each guarded against false positives.
- Optional config file (`$LOGX_CONFIG` / `$XDG_CONFIG_HOME/logx/config.yaml` /
  `~/.config/logx/config.yaml`) for default level/theme/color and custom
  level/message/timestamp field-name mappings. Precedence is flag > config >
  built-in default.
- Support init and ephemeral containers: `logx containers` now lists them
  (tagged `[init]`/`[ephemeral]`, and with a `kind` field in `-o json`/`-o yaml`),
  and `logx logs -c <init-or-ephemeral-container>` can fetch their logs.
- Recognize the klog/glog format used by Kubernetes components (e.g.
  `E0624 10:00:02.333 12 server.go:42] failed to sync`), mapping the I/W/E/F
  level letter to the log level instead of treating the line as level-less.
- `logx parse [file]` reads logs from a file or stdin and renders them with the
  same parsing, multi-line grouping, and `--level` filtering as `logx logs` — no
  cluster required. It also recognizes a leading `kubectl logs --timestamps`
  prefix (`kubectl logs pod | logx parse`).
- Grouped multi-line log entries: indented continuation lines (e.g. stack-trace
  frames) now inherit the level of the entry they belong to, so a stack trace
  stays visible when filtering at its parent's `--level` (e.g. `ERROR`). Flush-left
  lines without a level keep their own level and are not relabeled. Note: because
  a clean fix would need lookahead (incompatible with `--follow`), the grouping
  biases toward over-inclusion — an indented line appearing after an earlier
  higher-severity entry may inherit that level. This is intentional, to avoid ever
  hiding a stack frame.
- Added `--timeline` to show pod logs and Kubernetes events together sorted by timestamp for point-in-time troubleshooting.
- Added a `[notice]` line in `--timeline` when container logs are unavailable
  (e.g. `ImagePullBackOff`) but events are, and when the event list is truncated.
- Added a CI workflow running build, `go test -race`, `golangci-lint`, and
  `govulncheck` on pushes and pull requests.
- Added synthetic log, event, pod, and expected-output fixtures to cover log filtering, timeline output, multiline logs, and container formatting.
- Added golden output tests for deterministic log and timeline output.

## Security

- Upgraded the module Go target from `1.26.0` to `1.26.3` to address reachable Go standard library vulnerabilities reported by `govulncheck`.
- Bumped `golang.org/x/net` to `v0.55.0` to fix GO-2026-5026, which `govulncheck`
  flagged as reachable via the log-streaming path.
- Added terminal output sanitization for untrusted Kubernetes and log data, escaping control characters before printing.
- Hardened sanitization against Unicode visual-spoofing characters (bidirectional
  overrides, zero-width characters, line/paragraph separators).
- Sanitized log output, structured log fields, container names, images, statuses, pod names, and namespaces across all output formats, including `-o json`, `-o yaml`, and `-o posix`.

## Fixed

- Render logfmt and bracketed log lines as reconstructed message + fields instead
  of echoing the raw line, which previously duplicated the timestamp/level that
  logx already prints.
- Correctly label pino JSON logs (previously detected as logrus).
- Fixed `--level` so it now filters streamed logs by minimum severity.
- Stopped mis-parsing trailing URLs/paths (e.g. `/v1/items?page=2`) as `key=value`
  log fields; they now stay in the message text.
- Surfaced log-stream read errors in `--timeline` instead of silently dropping them.
- Normalized timeline timestamp output to UTC for stable log and event ordering across local time zones.
- Increased Kubernetes log scanner max line size to `1 MiB` to avoid failures on larger log entries.
