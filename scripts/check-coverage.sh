#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./scripts/check-coverage.sh [THRESHOLD]
  ./scripts/check-coverage.sh [--json] [--race] [--threshold N] [--threshold-file PATH]

Options:
  THRESHOLD            Backward-compatible positional default threshold.
  --json               Emit package results as JSON.
  --race               Run go test with race detector.
  --threshold N        Default coverage threshold (percent). Default: 80.
  --threshold-file P   Threshold config file. Default: coverage-thresholds.json.
  -h, --help           Show this help text.
EOF
}

DEFAULT_THRESHOLD="80"
THRESHOLD_FILE="coverage-thresholds.json"
JSON_OUTPUT=0
RACE_ENABLED=0
CLI_THRESHOLD_SET=0
COVERPROFILE="${COVERPROFILE:-coverage.out}"

if [[ $# -gt 0 && "$1" != -* ]]; then
  DEFAULT_THRESHOLD="$1"
  CLI_THRESHOLD_SET=1
  shift
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    --json)
      JSON_OUTPUT=1
      shift
      ;;
    --race)
      RACE_ENABLED=1
      shift
      ;;
    --threshold)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --threshold" >&2
        exit 2
      fi
      DEFAULT_THRESHOLD="$2"
      CLI_THRESHOLD_SET=1
      shift 2
      ;;
    --threshold-file)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --threshold-file" >&2
        exit 2
      fi
      THRESHOLD_FILE="$2"
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

if ! [[ "$DEFAULT_THRESHOLD" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  echo "invalid threshold: $DEFAULT_THRESHOLD" >&2
  exit 2
fi

TEST_OUTPUT_FILE="$(mktemp)"
PACKAGE_LIST_FILE="$(mktemp)"
RESULTS_FILE="$(mktemp)"
PARSER_FILE=""

cleanup() {
  rm -f "$TEST_OUTPUT_FILE" "$PACKAGE_LIST_FILE" "$RESULTS_FILE"
  if [[ -n "$PARSER_FILE" ]]; then
    rm -f "$PARSER_FILE"
  fi
}
trap cleanup EXIT

declare -A PACKAGE_THRESHOLD_OVERRIDES=()
declare -A PACKAGE_COVERAGE=()
declare -A PACKAGE_ERRORS=()
declare -A PACKAGE_HAS_COVERAGE_LINE=()

load_threshold_file() {
  local file="$1"
  [[ -f "$file" ]] || return 0

  PARSER_FILE="$(mktemp --suffix=.go)"
  cat >"$PARSER_FILE" <<'GOEOF'
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type richConfig struct {
	DefaultThreshold *float64           `json:"default_threshold"`
	Packages         map[string]float64 `json:"packages"`
}

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}

	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		os.Exit(1)
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}

	flat := map[string]float64{}
	if err := json.Unmarshal(raw, &flat); err == nil && len(flat) > 0 {
		keys := make([]string, 0, len(flat))
		for k := range flat {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s\t%.1f\n", k, flat[k])
		}
		return
	}

	var rich richConfig
	if err := json.Unmarshal(raw, &rich); err != nil {
		os.Exit(1)
	}
	if rich.DefaultThreshold != nil {
		fmt.Printf("DEFAULT\t%.1f\n", *rich.DefaultThreshold)
	}
	keys := make([]string, 0, len(rich.Packages))
	for k := range rich.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s\t%.1f\n", k, rich.Packages[k])
	}
}
GOEOF

  local parser_output
  if ! parser_output="$(go run "$PARSER_FILE" "$file" 2>/dev/null)"; then
    echo "failed to parse threshold file: $file" >&2
    exit 2
  fi

  while IFS=$'\t' read -r key value; do
    [[ -n "$key" ]] || continue
    if [[ "$key" == "DEFAULT" ]]; then
      if [[ "$CLI_THRESHOLD_SET" -eq 0 ]]; then
        DEFAULT_THRESHOLD="$value"
      fi
      continue
    fi
    PACKAGE_THRESHOLD_OVERRIDES["$key"]="$value"
  done <<<"$parser_output"
}

load_threshold_file "$THRESHOLD_FILE"

if ! go list ./internal/... ./cmd/... >"$PACKAGE_LIST_FILE"; then
  echo "failed to list coverage target packages" >&2
  exit 1
fi

TEST_EXIT=0
GO_TEST_ARGS=(
  -count=1
  -covermode=atomic
  -coverprofile="$COVERPROFILE"
)
if [[ "$RACE_ENABLED" -eq 1 ]]; then
  GO_TEST_ARGS+=(-race)
fi

go test "${GO_TEST_ARGS[@]}" ./internal/... ./cmd/... >"$TEST_OUTPUT_FILE" 2>&1 || TEST_EXIT=$?

while IFS= read -r line; do
  if [[ "$line" =~ ^ok[[:space:]]+([^[:space:]]+).*[[:space:]]coverage:[[:space:]]*([0-9]+([.][0-9]+)?)%[[:space:]]of[[:space:]]statements ]]; then
    pkg="${BASH_REMATCH[1]}"
    cov="${BASH_REMATCH[2]}"
    PACKAGE_COVERAGE["$pkg"]="$cov"
    PACKAGE_HAS_COVERAGE_LINE["$pkg"]=1
    continue
  fi
  if [[ "$line" =~ ^[[:space:]]*([^[:space:]]+)[[:space:]]+coverage:[[:space:]]*([0-9]+([.][0-9]+)?)%[[:space:]]of[[:space:]]statements ]]; then
    pkg="${BASH_REMATCH[1]}"
    cov="${BASH_REMATCH[2]}"
    PACKAGE_COVERAGE["$pkg"]="$cov"
    PACKAGE_HAS_COVERAGE_LINE["$pkg"]=1
    continue
  fi
  if [[ "$line" =~ ^FAIL[[:space:]]+([^[:space:]]+) ]]; then
    pkg="${BASH_REMATCH[1]}"
    PACKAGE_ERRORS["$pkg"]="build_or_test_failed"
  fi
done <"$TEST_OUTPUT_FILE"

PASS_COUNT=0
FAIL_COUNT=0

while IFS= read -r pkg; do
  [[ -n "$pkg" ]] || continue
  cov="${PACKAGE_COVERAGE[$pkg]:-0.0}"
  threshold="${PACKAGE_THRESHOLD_OVERRIDES[$pkg]:-$DEFAULT_THRESHOLD}"
  status="PASS"
  reason="ok"

  if [[ -n "${PACKAGE_ERRORS[$pkg]:-}" ]]; then
    status="ERROR"
    reason="${PACKAGE_ERRORS[$pkg]}"
  elif [[ -z "${PACKAGE_HAS_COVERAGE_LINE[$pkg]:-}" ]]; then
    status="FAIL"
    reason="no_coverage_data"
  elif awk -v c="$cov" -v t="$threshold" 'BEGIN { exit !(c+0 < t+0) }'; then
    status="FAIL"
    reason="below_threshold"
  fi

  if [[ "$status" == "PASS" ]]; then
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi

  printf "%s\t%s\t%s\t%s\t%s\n" "$pkg" "$cov" "$threshold" "$status" "$reason" >>"$RESULTS_FILE"
done <"$PACKAGE_LIST_FILE"

if [[ "$JSON_OUTPUT" -eq 1 ]]; then
  echo "["
  first=1
  while IFS=$'\t' read -r pkg cov threshold status reason; do
    if [[ "$first" -eq 0 ]]; then
      echo ","
    fi
    first=0
    printf '  {"package":"%s","coverage":%s,"threshold":%s,"status":"%s","reason":"%s"}' \
      "$pkg" "$cov" "$threshold" "$status" "$reason"
  done <"$RESULTS_FILE"
  echo
  echo "]"
else
  printf "%-56s %-10s %-10s %-8s %s\n" "PACKAGE" "CURRENT" "TARGET" "STATUS" "REASON"
  printf "%-56s %-10s %-10s %-8s %s\n" "--------------------------------------------------------" "----------" "----------" "--------" "-------------------------"
  while IFS=$'\t' read -r pkg cov threshold status reason; do
    printf "%-56s %-10s %-10s %-8s %s\n" "$pkg" "${cov}%" "${threshold}%" "$status" "$reason"
  done <"$RESULTS_FILE"
  echo
  echo "Summary: pass=$PASS_COUNT fail=$FAIL_COUNT threshold=${DEFAULT_THRESHOLD}%"
fi

if [[ "$FAIL_COUNT" -gt 0 || "$TEST_EXIT" -ne 0 ]]; then
  exit 1
fi

exit 0
