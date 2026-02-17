#!/usr/bin/env bash
set -euo pipefail

threshold="${1:-140}"

if command -v rg >/dev/null 2>&1; then
  mapfile -t files < <(rg --files cmd internal \
    --glob '*.go' \
    --glob '!**/*_test.go' \
    --glob '!**/*_generated.go')
else
  mapfile -t files < <(find cmd internal -type f -name '*.go' ! -name '*_test.go' ! -name '*_generated.go' | sort)
fi

if [[ ${#files[@]} -eq 0 ]]; then
  echo "No Go files found for duplicate check."
  exit 0
fi

echo "Running dupl (threshold=${threshold}) on ${#files[@]} files"
output="$(dupl -threshold "${threshold}" "${files[@]}" || true)"

if grep -qE '^found [0-9]+ clones:' <<<"${output}"; then
  echo "Duplicate code detected:"
  echo "${output}"
  exit 1
fi

echo "No duplicate code blocks detected."
