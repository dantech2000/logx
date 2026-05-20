# Release Notes

## Added

- Added `--timeline` to show pod logs and Kubernetes events together sorted by timestamp for point-in-time troubleshooting.
- Added synthetic log, event, pod, and expected-output fixtures to cover log filtering, timeline output, multiline logs, and container formatting.
- Added golden output tests for deterministic log and timeline output.

## Security

- Upgraded the module Go target from `1.26.0` to `1.26.3` to address reachable Go standard library vulnerabilities reported by `govulncheck`.
- Added terminal output sanitization for untrusted Kubernetes and log data, escaping control characters before printing.
- Sanitized log output, structured log fields, container names, images, statuses, pod names, and namespaces.

## Fixed

- Fixed `--level` so it now filters streamed logs by minimum severity.
- Normalized timeline timestamp output to UTC for stable log and event ordering across local time zones.
- Increased Kubernetes log scanner max line size to `1 MiB` to avoid failures on larger log entries.
