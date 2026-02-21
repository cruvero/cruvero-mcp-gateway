Run all project quality gates sequentially and report results.

## Instructions

1. Run each quality gate one at a time in this order, stopping on the **first failure** unless `fix` is passed as an argument:
   1. `go vet ./...`
   2. `golangci-lint run ./...`
   3. `staticcheck ./...`
   4. `govulncheck ./...`
   5. `./scripts/check-gosec.sh`
   6. `./scripts/check-dupl.sh`
   7. `./scripts/check-godoc.sh`
   8. `./scripts/check-coverage.sh`
2. For each gate, report: **PASS** or **FAIL** with a one-line summary.
3. If all gates pass, also run `go build ./cmd/mcpgw` as a final sanity check.

## Auto-fix Mode

If the argument is `fix` (`$ARGUMENTS` contains "fix"):

- When a gate fails, attempt to fix the issues automatically before moving to the next gate.
- For lint/vet issues: apply the fix directly in the source files.
- For godoc issues: add the missing doc comments following existing patterns.
- For coverage issues: identify uncovered functions and add targeted tests.
- After fixing, re-run the failed gate to confirm the fix works, then continue.

## Output Format

```
Quality Gates
─────────────
1. go vet          PASS
2. golangci-lint   PASS
3. staticcheck     FAIL — SA1019: deprecated API usage in internal/proxy/handler.go:42
   (stopping — run with 'fix' to auto-repair)
```

## Constraints

- Never skip a gate or suppress its output.
- Never mention AI, Claude, or LLM in any fixes or comments.
