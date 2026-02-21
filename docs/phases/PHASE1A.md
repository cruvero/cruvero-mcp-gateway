# Phase 1A: Project Skeleton, Types, Config, HTTP Server

## Overview

Bootstrap the Go module, establish the directory layout, define core types, implement environment-based configuration, and set up the HTTP server with chi router.

## Scope

### Go Module & Directory Layout
- Module: `github.com/cruvero/mcp-gateway`
- Directory structure following the repo layout in README.md
- Makefile targets: build, test, lint, vet, coverage
- CI skeleton (GitHub Actions): test, lint, build jobs
- .gitignore for Go projects

### Core Types (`internal/types/types.go`)
- `ServerRecord` — registered MCP server (id, name, spiffe_id, version, host, port, capabilities, status, policy_profile, last_heartbeat, created_at, updated_at)
- `Registration` — registration request payload (service_name, version, listen address, capabilities, labels)
- `Capability` — MCP capability descriptor (tools []string, resources []string, prompts []string)
- `PolicyProfile` — policy configuration (name, rate_limit, rate_burst, tool_allowlist, tool_denylist, enforcement_mode)
- `HealthStatus` — health check response (status, version, uptime, registered_servers, nats_connected)
- `ServerStatus` — enum: pending, approved, active, stale, expired
- All types with JSON tags

### Configuration (`internal/config/`)
- `Config` struct with all MCPGW_* fields
- `Load()` function reading from environment variables
- `Validate()` method checking required fields
- Default values for optional fields
- Environment variables:
  - MCPGW_LISTEN_ADDR (default :8443)
  - MCPGW_TLS_CERT, MCPGW_TLS_KEY, MCPGW_TLS_CA
  - MCPGW_DB_URL
  - MCPGW_NATS_URL (optional)
  - MCPGW_HEARTBEAT_TTL (default 30s)
  - MCPGW_RATE_DEFAULT (default 10), MCPGW_RATE_BURST (default 20)
  - MCPGW_LOG_FORMAT (default json), MCPGW_LOG_LEVEL (default info)
  - MCPGW_METRICS_ADDR (default :9090)
  - MCPGW_CRUVERO_ENABLED (default false)
  - MCPGW_GATEWAY_ID (default auto)

### HTTP Server (`internal/server/`)
- `Server` struct wrapping http.Server
- Chi router with middleware chain: request ID, logging, recovery, optional CORS
- Routes: /healthz (liveness), /readyz (readiness), /metrics (placeholder)
- `New(cfg *config.Config) *Server`
- `Start(ctx context.Context) error` — starts with graceful shutdown on context cancellation
- Enforce request size limits via `http.MaxBytesReader`
- Structured logging via slog (JSON format by default)

### Test Utilities (`internal/testutil/`)
- Add minimal certificate helpers in Phase 1 so mTLS tests in Phase 2 do not duplicate cert generation code
- `GenerateTestCerts(t *testing.T)` for CA/server/client fixtures

## Files Created

| File | Description |
|------|-------------|
| go.mod | Module definition |
| Makefile | Build targets |
| .github/workflows/ci.yml | CI pipeline skeleton |
| cmd/mcpgw/main.go | Entrypoint |
| internal/types/types.go | Core type definitions |
| internal/config/config.go | Config loading |
| internal/config/config_test.go | Config tests |
| internal/server/server.go | HTTP server + router |
| internal/server/middleware.go | Middleware chain |
| internal/server/server_test.go | Server tests |
| internal/testutil/certs.go | Minimal certificate test helpers |

## Testing Requirements

- Config: test Load() with env vars set, test Validate() with missing required fields, test defaults
- Server: test health endpoints return 200, test middleware chain (request ID present, panic recovery)
- Testutil: test certificate helper emits valid PEM and SPIFFE URI SAN client cert
- Coverage: >=80% per package
