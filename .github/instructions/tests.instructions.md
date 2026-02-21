---
applyTo: "**/*_test.go"
---

# Test Review Standards

## Structure

- Table-driven tests with `t.Run()` subtests are required for multi-case scenarios.
- Use `t.Parallel()` on every top-level test and subtest unless shared state prevents it.
- Test function names: `TestTypeName_MethodOrBehavior` or `TestFunctionName_Scenario`.

## Database and Environment

- Use `go-sqlmock` for database tests — never hit a real DB in unit tests.
- Use `t.Setenv()` for env-var tests — no global mutation.
- Build tags for non-unit suites: `integration`, `security`, `load`.

## Coverage

- Coverage must meet or exceed the per-package threshold (80%) defined in `coverage-thresholds.json`.

## Best Practices

- Flag any test that uses `time.Sleep` for synchronization — prefer channels, `sync.WaitGroup`, or retry loops.
- `t.Helper()` must be called in test helper functions.
- Test cleanup via `t.Cleanup()` — no deferred resource cleanup that could leak on `t.Fatal`.
- No assertion libraries; use standard `if` + `t.Fatalf` pattern.
