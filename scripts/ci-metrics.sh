#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/ci-metrics.sh [--repo owner/name] [--compare owner/name] [--workflow NAME] [--event NAME] [--limit N]

Options:
  --repo OWNER/REPO      Primary repository (default: inferred from git remote).
  --compare OWNER/REPO   Optional comparison repository.
  --workflow NAME        Workflow name (default: CI).
  --event NAME           GitHub Actions event (default: pull_request).
  --limit N              Number of runs to inspect (default: 30, max 100).
  -h, --help             Show help.
EOF
}

infer_repo() {
  local remote
  remote="$(git config --get remote.origin.url || true)"
  if [[ -z "$remote" ]]; then
    echo ""
    return
  fi
  if [[ "$remote" =~ github.com[:/]([^/]+)/([^/.]+)(\.git)?$ ]]; then
    echo "${BASH_REMATCH[1]}/${BASH_REMATCH[2]}"
    return
  fi
  echo ""
}

REPO="$(infer_repo)"
COMPARE_REPO=""
WORKFLOW="CI"
EVENT="pull_request"
LIMIT=30

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    --compare)
      COMPARE_REPO="${2:-}"
      shift 2
      ;;
    --workflow)
      WORKFLOW="${2:-}"
      shift 2
      ;;
    --event)
      EVENT="${2:-}"
      shift 2
      ;;
    --limit)
      LIMIT="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$REPO" ]]; then
  echo "unable to infer repository; pass --repo owner/name" >&2
  exit 2
fi

if ! [[ "$LIMIT" =~ ^[0-9]+$ ]] || (( LIMIT < 1 || LIMIT > 100 )); then
  echo "--limit must be an integer between 1 and 100" >&2
  exit 2
fi

print_stats() {
  local repo="$1"
  local workflow="$2"
  local event="$3"
  local limit="$4"

  local python_bin
  if command -v python3 >/dev/null 2>&1; then
    python_bin="python3"
  elif command -v python >/dev/null 2>&1; then
    python_bin="python"
  else
    echo "Error: python3 interpreter not found" >&2
    return 1
  fi

  local durations_json
  durations_json="$(gh run list -R "$repo" --workflow "$workflow" --event "$event" --limit "$limit" --json startedAt,updatedAt -q 'map(((.updatedAt|fromdateiso8601)-(.startedAt|fromdateiso8601))/60)')"

  "$python_bin" - "$repo" "$workflow" "$event" "$durations_json" <<'PY'
import json
import statistics
import sys

repo, workflow, event, raw = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
values = [float(v) for v in json.loads(raw)]

print(f"Repository: {repo}")
print(f"Workflow:   {workflow}")
print(f"Event:      {event}")
if not values:
    print("Runs:       0")
    print()
    return_code = 0
else:
    values_sorted = sorted(values)
    n = len(values_sorted)
    p50 = statistics.median(values_sorted)
    p90 = values_sorted[int(0.9 * (n - 1))]
    avg = sum(values_sorted) / n
    print(f"Runs:       {n}")
    print(f"Avg (min):  {avg:.2f}")
    print(f"P50 (min):  {p50:.2f}")
    print(f"P90 (min):  {p90:.2f}")
    print(f"Min (min):  {values_sorted[0]:.2f}")
    print(f"Max (min):  {values_sorted[-1]:.2f}")
    print()
PY
}

print_stats "$REPO" "$WORKFLOW" "$EVENT" "$LIMIT"
if [[ -n "$COMPARE_REPO" ]]; then
  print_stats "$COMPARE_REPO" "$WORKFLOW" "$EVENT" "$LIMIT"
fi
