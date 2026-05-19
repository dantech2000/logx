# logx

`logx` is an enhanced Kubernetes pod log viewer. It fetches pod logs, parses common structured and plain-text formats, highlights useful fields, and works as both a standalone CLI and a `kubectl` plugin.

## Features

- Smart log parsing for JSON and plain-text logs
- Timestamp and log-level detection
- Colorized, readable output
- Multi-container pod support with interactive selection
- Previous container logs with `-p`
- Live log following with `-f`
- Container listing with JSON, YAML, and POSIX output

## Installation

### Homebrew

```bash
brew tap dantech2000/tap
brew install --cask logx
```

### Manual

Prerequisites:

- Go 1.26 or later
- Access to a Kubernetes cluster
- `kubectl` configured with the appropriate context
- `just` command runner, optional

Build the binaries:

```bash
git clone https://github.com/dantech2000/logx.git
cd logx
just build
```

Or with Go directly:

```bash
go build -o bin/logx main.go
go build -o bin/kubectl-logx main.go
```

Move either binary into a directory on your `PATH`. Kubernetes discovers the plugin form from the `kubectl-logx` executable name:

```bash
logx --help
kubectl logx --help
```

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
- `-c, --container`: Container name for multi-container pods
- `-f, --follow`: Follow log output
- `-l, --level`: Filter logs by level: `DEBUG`, `INFO`, `WARN`, `ERROR`
- `-p, --previous`: Fetch logs from the previous terminated container instance

Example:

```bash
logx my-pod -n my-namespace -c my-container -f -l INFO
```

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

The release pipeline should publish both the standalone `logx` binary and the `kubectl-logx` plugin binary.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
