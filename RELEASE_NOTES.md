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

- Fixed `--level` so it now filters streamed logs by minimum severity.
- Stopped mis-parsing trailing URLs/paths (e.g. `/v1/items?page=2`) as `key=value`
  log fields; they now stay in the message text.
- Surfaced log-stream read errors in `--timeline` instead of silently dropping them.
- Normalized timeline timestamp output to UTC for stable log and event ordering across local time zones.
- Increased Kubernetes log scanner max line size to `1 MiB` to avoid failures on larger log entries.
