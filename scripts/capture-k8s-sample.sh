#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Capture Kubernetes pod logs, events, pod metadata, and logx timeline output.

Usage:
  scripts/capture-k8s-sample.sh --pod POD [--namespace NAMESPACE] [--out-dir DIR] [--level LEVEL]

Options:
  -p, --pod POD              Pod name to capture.
  -n, --namespace NAMESPACE  Namespace. Defaults to current kubectl namespace, then default.
  -o, --out-dir DIR          Output directory. Defaults to logx-samples/<namespace>/<pod>-<timestamp>.
  -l, --level LEVEL          logx level for timeline capture. Defaults to DEBUG.
  -h, --help                 Show this help.

Examples:
  scripts/capture-k8s-sample.sh --pod my-api-7d9c6f8f85-k2t4p --namespace production
  scripts/capture-k8s-sample.sh -p my-worker -n default -o logx-samples/my-worker
USAGE
}

pod=""
namespace=""
out_dir=""
level="DEBUG"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--pod)
      pod="${2:-}"
      shift 2
      ;;
    -n|--namespace)
      namespace="${2:-}"
      shift 2
      ;;
    -o|--out-dir)
      out_dir="${2:-}"
      shift 2
      ;;
    -l|--level)
      level="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$pod" ]]; then
  echo "Error: --pod is required." >&2
  usage >&2
  exit 2
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "Error: kubectl is not installed or not on PATH." >&2
  exit 1
fi

if [[ -z "$namespace" ]]; then
  namespace="$(kubectl config view --minify --output 'jsonpath={..namespace}')"
  namespace="${namespace:-default}"
fi

if [[ -z "$out_dir" ]]; then
  timestamp="$(date -u '+%Y%m%dT%H%M%SZ')"
  out_dir="logx-samples/${namespace}/${pod}-${timestamp}"
fi

mkdir -p "$out_dir"

run_capture() {
  local description="$1"
  local output_file="$2"
  shift 2

  echo "Capturing ${description} -> ${output_file}"
  if ! "$@" >"${output_file}" 2>"${output_file}.stderr"; then
    echo "Warning: failed to capture ${description}. See ${output_file}.stderr" >&2
    return 0
  fi
  if [[ ! -s "${output_file}.stderr" ]]; then
    rm -f "${output_file}.stderr"
  fi
}

sanitize_filename() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_'
}

run_capture "pod YAML" "${out_dir}/pod.yaml" \
  kubectl get pod "$pod" -n "$namespace" -o yaml

run_capture "pod JSON" "${out_dir}/pod.json" \
  kubectl get pod "$pod" -n "$namespace" -o json

run_capture "events YAML" "${out_dir}/events.yaml" \
  kubectl get events -n "$namespace" -o yaml

run_capture "events JSON" "${out_dir}/events.json" \
  kubectl get events -n "$namespace" -o json

run_capture "events table" "${out_dir}/events-table.txt" \
  kubectl get events -n "$namespace" --sort-by=.lastTimestamp

run_capture "pod logs with kubectl timestamps" "${out_dir}/logs.txt" \
  kubectl logs "$pod" -n "$namespace" --timestamps

run_capture "logx timeline ${level}" "${out_dir}/logx-timeline-${level}.txt" \
  kubectl logx "$pod" -n "$namespace" --timeline --level "$level"

containers_file="${out_dir}/containers.txt"
run_capture "container list" "$containers_file" \
  kubectl get pod "$pod" -n "$namespace" -o 'jsonpath={range .spec.containers[*]}{.name}{"\n"}{end}'

if [[ -s "$containers_file" ]]; then
  while IFS= read -r container; do
    [[ -z "$container" ]] && continue
    safe_container="$(sanitize_filename "$container")"

    run_capture "logs for container ${container}" "${out_dir}/logs-${safe_container}.txt" \
      kubectl logs "$pod" -n "$namespace" -c "$container" --timestamps

    run_capture "logx timeline ${level} for container ${container}" "${out_dir}/logx-timeline-${safe_container}-${level}.txt" \
      kubectl logx "$pod" -n "$namespace" -c "$container" --timeline --level "$level"

    run_capture "previous logs for container ${container}" "${out_dir}/logs-${safe_container}-previous.txt" \
      kubectl logs "$pod" -n "$namespace" -c "$container" --previous --timestamps
  done <"$containers_file"
fi

cat >"${out_dir}/capture-info.txt" <<EOF
pod=${pod}
namespace=${namespace}
level=${level}
captured_at_utc=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
kubectl_context=$(kubectl config current-context 2>/dev/null || true)
EOF

echo "Capture complete: ${out_dir}"
