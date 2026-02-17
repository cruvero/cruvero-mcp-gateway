#!/usr/bin/env bash
set -euo pipefail

echo "Checking exported Go declaration docs"
go run ./scripts/check-godoc.go
