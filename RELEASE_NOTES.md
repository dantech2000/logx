# Release Notes

## Security

- Upgraded the module Go target from `1.26.0` to `1.26.3` to address reachable Go standard library vulnerabilities reported by `govulncheck`.
- Added terminal output sanitization for untrusted Kubernetes and log data, escaping control characters before printing.
- Sanitized log output, structured log fields, container names, images, statuses, pod names, and namespaces.

## Fixed

- Fixed `--level` so it now filters streamed logs by minimum severity.
- Increased Kubernetes log scanner max line size to `1 MiB` to avoid failures on larger log entries.
