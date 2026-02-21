# Phase 8B Implementation Prompts

## Prompt 1 of 3: Test Utilities & Fixtures

### Required Reading (read these files before writing code)
- docs/phases/PHASE8B.md
- internal/store/interfaces.go
- internal/types/types.go
- internal/identity/tls.go

### Task

Create test utilities and fixtures.

1. `internal/testutil/testutil.go`:
   - Extend the Phase 1 cert helper instead of replacing it
   - SetupTestDB(t *testing.T) *sql.DB:
     - Use testing.Short() to skip if short mode
     - Read MCPGW_TEST_DB_URL env (default: postgres://localhost:5432/mcpgw_test?sslmode=disable)
     - Open connection
     - Run migrations using golang-migrate
     - t.Cleanup: drop all tables, close connection
   - SetupTestNATS(t *testing.T) (*nats.Conn, string):
     - Start embedded NATS server (nats-server/test)
     - Connect and return connection + URL
     - t.Cleanup: close connection, shutdown server
   - GenerateTestCerts(t *testing.T) TestCerts:
     - Generate CA key + self-signed CA cert
     - Generate server cert signed by CA
     - Generate client cert signed by CA with SPIFFE URI SAN
     - Return struct with all PEM-encoded certs and keys
   - TestCerts struct: CACert, ServerCert, ServerKey, ClientCert, ClientKey []byte, SPIFFEID string
   - NewTestServer(t *testing.T, handler http.Handler, certs TestCerts) *httptest.Server

2. `internal/testutil/fixtures.go`:
   - TestServerRecord() types.ServerRecord: valid defaults, status=active
   - TestRegistration() registration.RegistrationRequest: valid with tools
   - TestAPIKey() (types.APIKey, string): return APIKey struct + plaintext
   - TestPolicyProfiles() map[string]*types.PolicyProfile: default (10/20), premium (100/200), admin (unlimited)

3. Tests:
   - Test GenerateTestCerts produces valid cert chain
   - Test fixtures produce valid types

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Test helpers work with real Postgres (when available)
- Test certs enable mTLS testing
- Fixtures provide realistic test data

---

## Prompt 2 of 3: Integration & Security Tests

### Required Reading (read these files before writing code)
- docs/phases/PHASE8B.md
- internal/testutil/testutil.go
- internal/testutil/fixtures.go
- internal/registration/handler.go
- internal/proxy/server.go
- internal/auth/middleware.go

### Task

Write integration and security tests.

1. `internal/testutil/integration_test.go` (build tag: integration):
   - TestFullLifecycle:
     - Setup test DB and NATS
     - Create all stores, services, handlers
     - Create test HTTP server with full middleware chain
     - Register server via mTLS -> verify 201
     - List servers -> verify registered server present
     - Send heartbeat -> verify status transition
     - Deregister -> verify 204
     - Verify NATS events received for each step
   - TestAuthFlow:
     - Create API key via store
     - Request with valid key -> 200
     - Request with invalid key -> 401
     - Request with no auth -> 401

2. `internal/testutil/security_test.go` (build tag: security):
   - TestExpiredCert: generate expired cert, attempt mTLS -> fail
   - TestWrongCA: generate cert from different CA -> fail
   - TestNoSPIFFEID: cert without URI SAN -> 401
   - TestWrongSPIFFEDomain: wrong trust domain -> 403
   - TestRevokedAPIKey: revoke then use -> 401
   - TestMalformedJSON: send invalid JSON -> 400
   - TestOversizedBody: send >1MB body -> 413

3. `internal/testutil/policy_test.go`:
   - TestDangerousPatterns: table-driven test for each dangerous pattern
   - TestAllowlistEnforcement: tool not in allowlist -> blocked
   - TestDenylistEnforcement: tool in denylist -> blocked
   - TestAuditMode: violations logged but request allowed

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Integration tests cover full lifecycle
- Security tests verify all auth boundaries
- Policy tests cover all dangerous patterns
- All tests pass with proper setup

---

## Prompt 3 of 3: Load Tests, Coverage Script, CI Integration

### Required Reading (read these files before writing code)
- docs/phases/PHASE8B.md
- internal/testutil/testutil.go
- Makefile
- .github/workflows/ci.yml

### Task

Create load tests and coverage infrastructure.

1. `internal/testutil/load_test.go` (build tag: load):
   - TestSustainedThroughput:
     - Configure rate limit at 100 req/s
     - Send 100 req/s for 10 seconds from multiple goroutines
     - Verify ~100 succeed/sec and excess get 429
     - Allow 10% tolerance
   - TestBurst:
     - Configure rate limit 10 req/s burst 20
     - Send 30 requests simultaneously
     - Verify ~20 succeed (burst), ~10 get 429
   - TestCircuitBreakerUnderLoad:
     - Create backend that fails after N requests
     - Send continuous traffic
     - Verify circuit opens (requests fail fast)
     - Restore backend, verify circuit recovers

2. `internal/testutil/ratelimit_test.go`:
   - TestConcurrentRateLimiting:
     - 10 goroutines, each sending 100 requests
     - Verify total successful <= expected (rate * duration * tolerance)
   - TestPerClientIsolation:
     - Two clients with different identities
     - Each gets their own rate limit bucket

3. `scripts/check-coverage.sh`:
   ```bash
   #!/usr/bin/env bash
   set -euo pipefail
   THRESHOLD=${1:-80}
   FAILED=0
   echo "Coverage Report (minimum: ${THRESHOLD}%)"
   echo "---"
   for pkg in $(go list ./internal/... ./cmd/...); do
     coverage=$(go test -coverprofile=/dev/null -covermode=atomic "$pkg" 2>/dev/null \
       | grep -oP 'coverage: \K[0-9.]+' || echo "0")
     status="PASS"
     if (( $(echo "$coverage < $THRESHOLD" | bc -l) )); then
       status="FAIL"
       FAILED=1
     fi
     printf "%-50s %6s%% [%s]\n" "$pkg" "$coverage" "$status"
   done
   exit $FAILED
   ```

4. `coverage-thresholds.json`:
   - Map each package to 80.0 threshold

5. Update CI:
   - Add integration test job (with Postgres and NATS services)
   - Add coverage check step using check-coverage.sh
   - Add security test job

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Load tests validate rate limiting behavior
- Coverage script reports per-package coverage
- CI runs all test tiers
- All packages at >=80% coverage (once implementation complete)
- >=80% coverage for testutil package itself
