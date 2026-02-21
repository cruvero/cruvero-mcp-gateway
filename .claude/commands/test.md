Run tests with the appropriate configuration for this project.

## Instructions

Parse `$ARGUMENTS` to determine the test mode. Default to `unit` if no argument is given.

### Modes

| Argument | Command | Description |
|----------|---------|-------------|
| *(empty)* / `unit` | `go test -race -coverprofile=coverage.out ./...` | All unit tests with race detection |
| `integration` | `go test -race -tags integration ./internal/testutil` | Integration test suite |
| `security` | `go test -race -tags security ./internal/testutil` | Security test suite |
| `load` | `go test -tags load ./internal/testutil` | Load test suite (no race — too slow) |
| `coverage` | `go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` | Unit tests + coverage report |
| `<package>` | `go test -race -v ./internal/<package>/...` | Test a specific package |
| `<package> <TestName>` | `go test -race -v -run <TestName> ./internal/<package>/...` | Run a specific test |

### Behavior

1. Detect which mode applies from the arguments.
2. Run the command and capture output.
3. Summarize results:
   - Total tests run, passed, failed, skipped
   - For failures: show the failing test name, file:line, and the failure message
   - For coverage mode: show per-package coverage percentages

### Special Handling

- If a specific package is given (e.g., `store`), resolve it to `./internal/store/...`.
- If a test name pattern is given (e.g., `store TestServerCRUD`), use `-run TestServerCRUD`.
- If tests fail, analyze the failures and suggest fixes.

## Constraints

- Always use `-race` except for load tests.
- Never mention AI, Claude, or LLM in any generated test code.
