# logx

`logx` is an enhanced Kubernetes pod log viewer. It fetches pod logs, parses common structured and plain-text formats, highlights useful fields, and works as both a standalone CLI and a `kubectl` plugin.

## Features

- Smart log parsing for JSON, logfmt, bracketed, klog/glog, common/combined and Envoy access logs, syslog priority, and plain-text logs
- Timestamp and log-level detection
- Colorized, readable output
- Multi-container pod support with interactive selection
- Previous container logs with `-p`
- Live log following with `-f`
- Container listing with JSON, YAML, and POSIX output
- Parsing logs from a file or stdin (no cluster needed) via `logx parse`

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

- `-n, --namespace`: Kubernetes namespace, defaults to the current context namespace
- `--context`: Kubernetes context, defaults to the current kubeconfig context
- `--kubeconfig`: Path to a kubeconfig file, defaults to `KUBECONFIG` or `~/.kube/config`
- `-c, --container`: Container name for multi-container pods
- `-f, --follow`: Follow log output
- `-l, --level`: Filter logs by level: `DEBUG`, `INFO`, `WARN`, `ERROR`
- `-p, --previous`: Fetch logs from the previous terminated container instance
- `--timeline`: Show pod logs and Kubernetes events together sorted by time

Example:

```bash
logx my-pod -n my-namespace -c my-container -f -l INFO
```

### Log Level Filters

`-l, --level` keeps logs at the selected level and above. For example, `-l WARN` shows `WARN` and `ERROR` entries while hiding `DEBUG` and `INFO`.

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
```

Only events for the target pod are shown. Repeated events are annotated with
their occurrence count (e.g. `(x3)`). If logs cannot be read but events are
available (such as `ImagePullBackOff`), the events are still shown with a
`[notice]` explaining that logs were unavailable.

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

`parse` runs the same parsing, multi-line grouping, and `--level` filtering as
`logx logs`, so it is handy for inspecting captured logs or piping output from
other tools. It also understands a leading `kubectl logs --timestamps` prefix.

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
