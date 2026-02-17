#!/usr/bin/env bash
set -euo pipefail

THRESHOLD_DEFAULT="80"
THRESHOLD="${1:-$THRESHOLD_DEFAULT}"
FAILED=0

echo "Coverage Report (minimum: ${THRESHOLD}%)"
echo "---"

packages=$(go list ./internal/... ./cmd/...)
for pkg in $packages; do
  output=$(go test -covermode=atomic -cover "$pkg" 2>/dev/null || true)
  coverage=$(echo "$output" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p' | tail -1)
  if [[ -z "$coverage" ]]; then
    coverage="0"
  fi

  status="PASS"
  if awk "BEGIN { exit !($coverage < $THRESHOLD) }"; then
    status="FAIL"
    FAILED=1
  fi

  printf "%-60s %6s%% [%s]\n" "$pkg" "$coverage" "$status"
done

exit $FAILED
