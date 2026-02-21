# Phase 1: Core Foundation

## Goal

Establish the buildable Go project with HTTP server, configuration system, core types, and Postgres persistence layer.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [1A](PHASE1A.md) | Project Skeleton, Types, Config, HTTP Server | [4 prompts](PHASE1A-PROMPT.md) | config, server, types |
| [1B](PHASE1B.md) | Postgres Store & Migrations | [3 prompts](PHASE1B-PROMPT.md) | store |

## Dependencies

None — this is the foundational phase.

## Deliverables

- Go module with `cmd/mcpgw` entrypoint
- Core types: ServerRecord, Registration, Capability, PolicyProfile, HealthStatus
- Environment-based configuration (MCPGW_* prefix)
- Chi HTTP router with middleware chain
- Health probes (/healthz, /readyz)
- Structured logging (slog)
- Postgres store with migrations (mcp_servers, api_keys, audit_log)
- Minimal `internal/testutil` cert helpers for downstream mTLS tests
- Makefile and CI skeleton

## Packages Created

- `internal/config` — Environment variable loading, validation
- `internal/server` — HTTP server, router setup, middleware
- `internal/store` — Postgres store interfaces and implementations
- `internal/testutil` — Foundational cert helpers for test reuse
- `internal/types` — Shared type definitions

## Success Criteria

- `go build ./cmd/mcpgw` succeeds
- `go test ./...` passes with >=80% coverage per package
- Server starts, responds to /healthz and /readyz
- Migrations run successfully against Postgres
- `go vet` and `golangci-lint` pass clean
