# Phase 8B: Integration Tests, Load Tests, Coverage

## Overview

Implement comprehensive integration tests, load tests, and security tests. Establish the coverage gate and ensure all packages meet the 80% minimum threshold.

## Scope

### Test Utilities (`internal/testutil/`)
- Extend the minimal cert helper introduced in Phase 1 instead of recreating it.
- `testutil.go`:
  - SetupTestDB(t *testing.T) *sql.DB: create test database, run migrations, return connection, cleanup on t.Cleanup
  - SetupTestNATS(t *testing.T) *nats.Conn: start embedded NATS server, return connection
  - GenerateTestCerts(t *testing.T) (caCert, serverCert, serverKey, clientCert, clientKey []byte): generate self-signed CA + server + client certs for testing
  - NewTestServer(t *testing.T, handler http.Handler) *httptest.Server: create HTTPS test server with test certs
- `fixtures.go`:
  - TestServerRecord(): create a sample ServerRecord with valid defaults
  - TestRegistration(): create a sample RegistrationRequest
  - TestAPIKey(): create a sample APIKey
  - TestPolicyProfile(): create sample policy profiles (default, premium, admin)

### Integration Tests (`internal/testutil/integration_test.go`)
Build tag: `//go:build integration`

**Full Lifecycle Test**
1. Start gateway with test Postgres and test NATS
2. Generate mTLS test certs
3. Register an MCP server via POST /v1/registrations with mTLS
4. Verify server appears in GET /v1/registrations
5. Send heartbeat, verify status transitions (approved -> active)
6. Call tools/list via MCP proxy, verify tool appears
7. Call tools/call, verify routing to backend and response
8. Stop heartbeats, wait for sweep, verify stale -> expired transitions
9. Deregister server, verify removal
10. Verify NATS events published for each lifecycle step

**Auth Integration Test**
1. Test mTLS with valid client cert -> 200
2. Test mTLS with invalid cert -> TLS handshake failure
3. Test API key creation, then request with key -> 200
4. Test request with expired key -> 403
5. Test request with no auth -> 401

**Config Sync Integration Test**
1. Start gateway with NATS
2. Publish policy config update via NATS
3. Verify gateway applies new policy
4. Disconnect NATS, verify gateway continues with cached config
5. Reconnect, verify gateway requests config snapshot

### Rate Limiting Tests (`internal/testutil/ratelimit_test.go`)
- Concurrent load test: N goroutines sending requests
- Verify rate limit enforced (expected 429 rate)
- Verify different policy profiles get different rates
- Verify rate limit headers present and accurate
- Measure actual throughput vs configured limit (within tolerance)

### Policy Tests (`internal/testutil/policy_test.go`)
- Test each dangerous command pattern blocks in enforce mode
- Test allowlist enforcement
- Test denylist enforcement
- Test audit mode allows but logs
- Test argument validation against schemas

### Security Tests (`internal/testutil/security_test.go`)
Build tag: `//go:build security`

- Cert validation edge cases:
  - Expired certificate -> rejection
  - Self-signed without CA trust -> rejection
  - Wrong trust domain in SPIFFE ID -> rejection
  - Cert with no URI SAN -> rejection
- Auth bypass attempts:
  - Missing Authorization header -> 401
  - Malformed Bearer token -> 401
  - API key for wrong scope -> 403
  - Replay of revoked API key -> 401
- Input validation:
  - Oversized request body -> 413 or 400
  - Malformed JSON -> 400
  - SQL injection in query params -> no injection (parameterized queries)

### Load Tests (`internal/testutil/load_test.go`)
Build tag: `//go:build load`

- Sustained throughput test: N requests/sec for M seconds
- Burst test: spike of N requests, measure rate limiting behavior
- Connection pool test: verify connection reuse under load
- Circuit breaker test: backend failures trigger circuit open, recovery after timeout

### Coverage Infrastructure
- `scripts/check-coverage.sh`:
  - Run `go test -coverprofile` per package
  - Parse coverage percentages
  - Fail if any package below 80%
  - Output summary table
- `coverage-thresholds.json`:
  - Per-package thresholds (all 80% initially)

## Files Created

| File | Description |
|------|-------------|
| internal/testutil/testutil.go | Test database, NATS, cert setup |
| internal/testutil/fixtures.go | Test data fixtures |
| internal/testutil/integration_test.go | Full lifecycle integration tests |
| internal/testutil/ratelimit_test.go | Rate limiting load tests |
| internal/testutil/policy_test.go | Policy enforcement tests |
| internal/testutil/security_test.go | Security boundary tests |
| internal/testutil/load_test.go | Load and stress tests |
| scripts/check-coverage.sh | Coverage gate script |
| coverage-thresholds.json | Per-package coverage thresholds |

## Testing Requirements

This IS the testing phase -- all tests described above must pass:
- Integration tests with real Postgres and NATS
- Rate limiting tests under concurrent load
- Policy tests for all dangerous patterns
- Security tests for auth boundaries
- Load tests for sustained throughput
- Coverage gate: every package >=80%
