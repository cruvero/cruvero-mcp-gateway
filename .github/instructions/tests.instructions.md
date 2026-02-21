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

## Mock Store Pattern

- Mock stores must implement the full interface with a compile-time check: `var _ store.X = (*mockX)(nil)`.
- Mock structs should include call-tracking fields (e.g., `listCalled bool`) and injectable error fields (e.g., `listErr error`).
- Use a `setupTestHandlerWithStores` or similar helper to wire mock dependencies into handlers.

## Admin Handler Testing

- Tests for HTMX partial responses must set the `HX-Request: true` header and verify the response does not contain `<!DOCTYPE`.
- Chi route parameters must be injected via `chi.RouteContext` in the request context.
- Session context must be injected via a `withSession` helper or equivalent middleware stub.
- Test both authenticated and unauthenticated request paths for every admin endpoint.

## Cryptographic Testing

- Encrypt/decrypt operations must be tested with round-trip assertions (encrypt then decrypt must equal original).
- Nonce uniqueness must be verified: encrypting the same plaintext twice must produce different ciphertexts.
- Tamper detection must be tested: flipping a byte in the ciphertext must cause decryption to fail.
- Expired session detection must be tested with a session whose expiry is in the past.
