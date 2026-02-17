#!/usr/bin/env bash
set -euo pipefail

severity="${1:-high}"
confidence="${2:-medium}"

echo "Running gosec (severity >= ${severity}, confidence >= ${confidence})"
gosec -quiet -severity "${severity}" -confidence "${confidence}" ./cmd/... ./internal/...
