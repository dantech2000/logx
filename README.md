# logx

`logx` is an enhanced Kubernetes pod log viewer. It fetches pod logs, parses common structured and plain-text formats, highlights useful fields, and works as both a standalone CLI and a `kubectl` plugin.

## Features

- Smart log parsing for JSON, logfmt, bracketed, klog/glog, common/combined and Envoy access logs, syslog priority, XML, CSV, flow-style YAML, and plain-text logs
- Timestamp and log-level detection across the full `TRACE → DEBUG → INFO → WARN → ERROR → FATAL` spectrum
- Colorized, readable output with dark/light themes and `--no-color`/`NO_COLOR` support
- Content filtering: `--grep`/`--exclude` regexes (with match highlighting) and `--where` field predicates (`status>=500`, `level>=WARN`)
- Field projection (`--fields ts,level,msg`) and machine-readable `--output json` (NDJSON)
- `--stats` summary digest: counts by level, HTTP status class, and the top recurring messages
- Server-side log windowing: `--since`, `--tail`, and `--timestamps`
- Multi-stream tailing: `--all-containers`, label-selector `--selector`, and `--all-namespaces`, with color-coded prefixes
- Multi-container pod support with interactive selection, including init and ephemeral containers
- Previous container logs with `-p`
- Live log following with `-f`
- `--timeline`: pod logs and Kubernetes events together, with container termination details (exit code / OOMKilled)
- Container listing with JSON, YAML, and POSIX output
- Parsing logs from a file or stdin (no cluster needed) via `logx parse` — the same engine and flags as `logx logs`
- Optional config file (`~/.config/logx/config.yaml`) for defaults and custom field-name mappings

## Installation

### Homebrew

```bash
brew tap dantech2000/tap
brew install --cask logx
```

The cask installs both `logx` and `kubectl-logx`. Because `kubectl-logx` is placed on your `PATH`, kubectl automatically discovers it as a plugin:

```bash
kubectl logx --help
kubectl plugin list
```

### Release Binaries

Download the archive for your platform from the GitHub releases page, then place the binaries on your `PATH`:

- `logx` for standalone use
- `kubectl-logx` for `kubectl logx`

Kubernetes discovers the plugin form from the `kubectl-logx` executable name. No plugin registration is required.

## Usage

Fetch logs from a pod:

```bash
logx my-pod -n my-namespace
kubectl logx my-pod -n my-namespace
```

The explicit subcommand form is also supported:

```bash
logx logs my-pod -n my-namespace
```

Options:

Selection and connection:

- `-n, --namespace`: Kubernetes namespace, defaults to the current context namespace
- `--context`: Kubernetes context, defaults to the current kubeconfig context
- `--kubeconfig`: Path to a kubeconfig file, defaults to `KUBECONFIG` or `~/.kube/config`
- `-c, --container`: Container name for multi-container pods
- `-a, --all-containers`: Stream every container in the pod, prefixed by container name
- `--selector`: Label selector (e.g. `app=api`); streams logs from all matching pods
- `-A, --all-namespaces`: With `--selector`, match pods across all namespaces

Log window and streaming:

- `-f, --follow`: Follow log output
- `--since`: Only show logs newer than a duration (e.g. `5m`, `2h`) or an RFC3339 time
- `--tail`: Show only the last N lines (`-1` for all)
- `--timestamps`: Include timestamps on each line
- `-p, --previous`: Fetch logs from the previous terminated container instance
- `--timeline`: Show pod logs and Kubernetes events together sorted by time

Filtering and output:

- `-l, --level`: Filter logs by level: `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL` (keeps the level and above)
- `-g, --grep`: Show only lines matching a regex (repeatable; OR). Matches are highlighted
- `--exclude`: Hide lines matching a regex (repeatable)
- `--highlight`: Highlight `--grep` matches (default true)
- `-w, --where`: Keep entries matching a field predicate, e.g. `status>=500` (repeatable; AND)
- `-F, --fields`: Project output to only these fields, e.g. `ts,level,msg`
- `-o, --output`: Output format: `text` (default) or `json` (NDJSON)
- `--stats`: Print a summary digest instead of the lines

Appearance (any command):

- `--color`: `auto` (default), `always`, or `never`
- `--no-color`: Disable color (alias for `--color=never`; `NO_COLOR` is also honored)
- `--theme`: `dark` (default) or `light`

Example:

```bash
logx my-pod -n my-namespace -c my-container -f -l INFO
```

### Log Level Filters

`-l, --level` keeps logs at the selected level and above. The full spectrum is
`TRACE < DEBUG < INFO < WARN < ERROR < FATAL`. For example, `-l WARN` shows
`WARN`, `ERROR`, and `FATAL` entries while hiding `TRACE`, `DEBUG`, and `INFO`.
`TRACE` sits below the default `DEBUG`, so trace-level lines are opt-in via
`-l TRACE`.

Plain text logs:

```bash
kubectl logx my-pod -n my-namespace -l WARN
```

Matches lines such as:

```text
2026-05-15T00:38:03Z WARN upstream latency high
2026-05-15T00:38:04Z ERROR upstream unavailable
```

JSON logs:

```bash
kubectl logx api-pod -n my-namespace -l ERROR
```

Matches fields such as:

```json
{"level":"error","ts":"2026-05-15T00:38:04Z","msg":"request failed","error":"timeout"}
```

Logfmt logs:

```bash
kubectl logx worker-pod -n my-namespace -l WARN
```

Matches fields such as:

```text
time=2026-05-15T00:38:03Z level=warn component=worker msg="retry scheduled"
time=2026-05-15T00:38:04Z level=error component=worker msg="job failed"
```

Bracketed logs:

```bash
kubectl logx traefik-pod -n my-namespace -l WARN
```

Matches lines such as:

```text
[2026-05-15 00:38:04] [WARN] [unknown] Traefik can reject some encoded characters in the request path
[2026-05-15 00:38:05] [ERROR] [unknown] Provider failed to sync providerName=kubernetes
```

### Search And Content Filters

`--grep`/`--exclude` filter by regex over each line, and matches are highlighted
in the output. Both are repeatable; `--grep` is OR (keep if any matches),
`--exclude` drops any match.

```bash
# Only request lines, minus the noisy health checks
logx my-pod --grep '/api/' --exclude '/healthz'

# Multiple --grep patterns (OR)
logx my-pod --grep 'timeout' --grep 'refused'
```

These work in `logx parse` too, so you can use them on captured logs:

```bash
cat app.log | logx parse --grep ERROR --exclude deprecated
```

### Field Predicates And Projection

`--where` keeps entries matching a predicate over a structured field or a virtual
key (`level`, `message`, `logger`, `ts`). Operators: `==`, `!=`, `>`, `>=`, `<`,
`<=`, and `~=` (regex). The `level` key compares by severity, so `level>=WARN`
works. Multiple `--where` are ANDed.

```bash
# 5xx responses only
logx my-pod --where 'status>=500'

# warnings and worse whose path looks like an API call
logx my-pod --where 'level>=WARN' --where 'path~=/api'
```

`--fields` projects the output down to chosen keys, in order:

```bash
logx my-pod --where 'status>=500' --fields ts,level,status,path,msg
# ts=2026-06-24 10:00:01 level=ERROR status=503 path=/api/orders msg="upstream timeout"
```

### Output Formats And Stats

`--output json` emits one normalized JSON object per line (NDJSON), ideal for
piping into `jq`. Fields already shown as columns are not duplicated, and the
encoder escapes control bytes so the output stays terminal-safe.

```bash
logx my-pod -o json | jq 'select(.level=="ERROR") | .fields.trace_id'
```

`--stats` replaces the per-line output with a digest — counts by level, HTTP
status class, and the most frequent (templated) messages:

```bash
logx my-pod --stats
# ── logx stats ──
# lines: 1204   span: 2026-06-24 10:00:00 → 10:05:00
# levels: ERROR 12  WARN 30  INFO 1160  DEBUG 2
# status: 2xx 1000  4xx 30  5xx 12
# top messages:
#    412  request # failed for user #
#     88  connection reset by peer
```

### Multiple Containers, Pods, And Namespaces

```bash
# Every container in one pod (init + ephemeral included), prefixed by name
logx my-pod --all-containers

# Every pod matching a label selector, prefixed by pod (or pod/container)
logx --selector app=api -n my-namespace

# Across all namespaces (requires --selector), prefixed by namespace/pod
logx --selector app=api --all-namespaces
```

Streams are read concurrently and merged with color-coded prefixes; writes are
serialized so lines never interleave.

### Log Window

```bash
# Last 5 minutes, last 200 lines, with timestamps
logx my-pod --since 5m --tail 200 --timestamps

# Everything since a specific time
logx my-pod --since 2026-06-24T10:00:00Z
```

### Logs And Events Timeline

Use `--timeline` to view pod logs and Kubernetes events together in timestamp order:

```bash
kubectl logx my-pod -n my-namespace -c my-container --timeline -l WARN
```

Example output:

```text
[2026-05-15 00:38:01] [EVENT] [Normal] pod/my-pod Scheduled: Successfully assigned default/my-pod
[2026-05-15 00:38:03] [EVENT] [Warning] pod/my-pod Unhealthy: Readiness probe failed
[2026-05-15 00:38:04] [LOG] [ERROR] request failed
[2026-05-15 00:38:05] [EVENT] [Warning] pod/my-pod BackOff: Back-off restarting failed container (x3)
[2026-05-15 00:38:10] [TERM] (previous) container "my-container" exited with code 137 — OOMKilled
```

Only events for the target pod are shown. Repeated events are annotated with
their occurrence count (e.g. `(x3)`). `[TERM]` lines surface container
termination details — exit code, signal, and reason such as `OOMKilled` — for
both the current and previous (`LastTerminationState`) instances, which the
events alone do not spell out. If logs cannot be read but events are available
(such as `ImagePullBackOff`), the events are still shown with a `[notice]`
explaining that logs were unavailable.

`--timeline` is intended for point-in-time troubleshooting and cannot be combined with `--follow`.

List containers in a pod:

```bash
logx containers my-pod -n my-namespace
```

Output formats:

```bash
logx containers my-pod -o json
logx containers my-pod -o yaml
logx containers my-pod -o posix
```

Parse logs from a file or stdin (no cluster required):

```bash
logx parse app.log
logx parse app.log -l WARN
kubectl logs my-pod | logx parse -l ERROR
cat app.log | logx parse
```

`parse` runs the same engine as `logx logs` — parsing, multi-line grouping, and
the full set of filtering and output flags (`--level`, `--grep`/`--exclude`,
`--where`, `--fields`, `--output json`, `--stats`, `--color`/`--theme`) — so it
is handy for inspecting captured logs or piping output from other tools. It also
understands a leading `kubectl logs --timestamps` prefix.

```bash
cat app.log | logx parse --where 'status>=500' --fields ts,status,path -o json
cat app.log | logx parse --stats
```

### Configuration

logx reads an optional config file for defaults and custom field-name mappings.
The location is `$LOGX_CONFIG`, else `$XDG_CONFIG_HOME/logx/config.yaml`, else
`~/.config/logx/config.yaml`. Every key is optional; an explicit flag always
overrides the config, which in turn overrides the built-in default.

```yaml
# ~/.config/logx/config.yaml
level: INFO       # default --level
theme: dark       # default --theme (dark | light)
color: auto       # default --color (auto | always | never)

# Teach the parser your team's bespoke JSON field names (additive)
fields:
  level: [lvl, "@l"]
  message: [text]
  timestamp: [event_time]
```

Version information:

```bash
logx version
logx version --short
logx version --output json
logx version --output yaml
```

Shell completion:

```bash
logx completion zsh > "${fpath[1]}/_logx"
```

## Development

Prerequisites:

Use either asdf or Nix for a consistent toolchain.

With asdf:

```bash
asdf install
```

With Nix flakes:

```bash
nix develop
```

Manual prerequisites:

- Go 1.26 or later
- Access to a Kubernetes cluster for manual testing
- `kubectl` configured with the appropriate context
- `just` command runner

Build from source:

```bash
git clone https://github.com/dantech2000/logx.git
cd logx
just build
```

Common tasks:

```bash
just --list
just build
just test
just fmt
```

`just build` creates:

- `bin/logx`
- `bin/kubectl-logx`

## Release

Create and push a version tag:

```bash
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

The release pipeline publishes both the standalone `logx` binary and the `kubectl-logx` plugin binary. It also updates the Homebrew cask in `dantech2000/homebrew-tap`.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
