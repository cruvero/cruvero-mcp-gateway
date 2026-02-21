# Phase 1A Implementation Prompts

## Prompt 1 of 4: Go Module, Directory Layout, Makefile

### Required Reading (read these files before writing code)
- README.md (repo layout)
- LLM.md (conventions)

### Task

Initialize the Go module and project skeleton.

1. Create `go.mod` with module path `github.com/cruvero/mcp-gateway` and Go 1.23+
2. Create directory structure:
   - cmd/mcpgw/
   - internal/auth/, internal/config/, internal/events/, internal/identity/
   - internal/policy/, internal/proxy/, internal/ratelimit/, internal/registration/
   - internal/resilience/, internal/server/, internal/store/, internal/testutil/, internal/types/
   - migrations/
   - charts/mcpgateway/
   - deploy/
   - scripts/
3. Create `Makefile` with targets:
   - `build`: `go build -trimpath -ldflags="-s -w" -o bin/mcpgw ./cmd/mcpgw`
   - `test`: `go test -race -coverprofile=coverage.out ./...`
   - `lint`: `golangci-lint run ./...`
   - `vet`: `go vet ./...`
   - `coverage`: `go tool cover -func=coverage.out`
   - `migrate-up`: placeholder for running migrations
   - `migrate-down`: placeholder
   - `clean`: remove build artifacts
4. Create `.gitignore` for Go (bin/, coverage.out, *.exe, .env, vendor/, etc.)
5. Create `.github/workflows/ci.yml` skeleton with test, lint, build jobs
6. Create `internal/testutil/certs.go` with a minimal `GenerateTestCerts(t *testing.T)` helper for mTLS tests used in Phase 2

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- `go mod tidy` runs without error
- `make build` would succeed once main.go exists
- Directory structure matches README.md layout

---

## Prompt 2 of 4: Core Types

### Required Reading (read these files before writing code)
- docs/phases/PHASE1A.md (types section)
- LLM.md (conventions)

### Task

Create `internal/types/types.go` with all core type definitions.

1. Define `ServerStatus` string type with constants: StatusPending, StatusApproved, StatusActive, StatusStale, StatusExpired
2. Define `ServerRecord` struct with all fields and JSON tags
3. Define `Registration` struct (request payload) with JSON tags
4. Define `Capability` struct with JSON tags
5. Define `PolicyProfile` struct with JSON tags and enforcement mode (enforce/audit)
6. Define `HealthStatus` struct with JSON tags
7. Define `EnforcementMode` string type with constants: ModeEnforce, ModeAudit
8. Add `String()` methods for enum types
9. Add `IsTerminal()` method on ServerStatus (returns true for expired)
10. Add `IsRoutable()` method on ServerStatus (returns true for active only)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All types have JSON tags
- All exported types have Go doc comments
- `go vet ./internal/types/...` passes
- Types are serializable to/from JSON

---

## Prompt 3 of 4: Configuration

### Required Reading (read these files before writing code)
- docs/phases/PHASE1A.md (config section)
- internal/types/types.go
- LLM.md (conventions)

### Task

Create `internal/config/config.go` and `internal/config/config_test.go`.

1. Define `Config` struct with all MCPGW_* fields, using appropriate Go types (time.Duration for TTLs, int for rates, bool for flags)
2. Implement `Load() (*Config, error)` that reads from os.Getenv
   - Parse duration strings for MCPGW_HEARTBEAT_TTL, MCPGW_CIRCUIT_TIMEOUT
   - Parse integers for MCPGW_RATE_DEFAULT, MCPGW_RATE_BURST, MCPGW_CIRCUIT_THRESHOLD, MCPGW_RETRY_MAX
   - Parse bool for MCPGW_CRUVERO_ENABLED
   - Apply defaults for optional fields
   - Generate UUID for MCPGW_GATEWAY_ID if set to "auto"
3. Implement `Validate() error` that checks:
   - MCPGW_DB_URL is required
   - If TLS is configured, cert and key must both be present
   - Rate values must be positive
   - NATS URL required if CRUVERO_ENABLED is true
4. Tests: cover all env vars, defaults, validation errors, duration parsing

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All env vars from the table are supported
- Defaults applied correctly
- Validation catches missing required fields
- >=80% test coverage

---

## Prompt 4 of 4: HTTP Server & Middleware

### Required Reading (read these files before writing code)
- docs/phases/PHASE1A.md (server section)
- internal/config/config.go
- internal/types/types.go
- LLM.md (conventions)

### Task

Create the HTTP server and middleware in `internal/server/`.

1. `server.go`:
   - `Server` struct with config, logger, router, http.Server
   - `New(cfg *config.Config, logger *slog.Logger) *Server`
   - `Start(ctx context.Context) error` — starts HTTPS if TLS configured, HTTP otherwise; graceful shutdown on context done
   - `setupRoutes()` — mount health, readyz, metrics placeholder routes
   - `/healthz` handler: returns 200 with `{"status":"ok"}`
   - `/readyz` handler: returns 200 if ready (checks can be added later), 503 if not
2. `middleware.go`:
   - Request ID middleware (X-Request-ID header, generate UUID if not present)
   - Logging middleware (slog: method, path, status, duration, request_id)
   - Recovery middleware (catch panics, log, return 500)
   - Request body size limit middleware using `http.MaxBytesReader` for write endpoints
   - CORS middleware (opt-in via config; disabled by default)
3. `server_test.go`:
   - Test /healthz returns 200 with correct body
   - Test /readyz returns 200
   - Test request ID is generated and present in response
   - Test recovery middleware catches panics
   - Test Start with context cancellation triggers graceful shutdown

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Server starts and serves health endpoints
- All middleware functions correctly
- Graceful shutdown works
- >=80% test coverage
