# Phase 10: Production Hardening & Tool Risk Classification

## Goal

Fix all P0 blockers that prevent production traffic, then implement a database-backed tool risk classification system that deterministically blocks destructive tool calls without LLM involvement.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [10A](PHASE10A.md) | P0 Bug Fixes: Audit Wiring, DB Pool, CORS, Retention | [4 prompts](PHASE10A-PROMPT.md) | server, config, store, policy |
| [10B](PHASE10B.md) | Tool Risk Classification & Policy Engine Integration | [4 prompts](PHASE10B-PROMPT.md) | store, policy, registration |

## Dependencies

- Phase 1–9 (complete, functional gateway)
- Phase 5B specifically (policy engine, audit logging)
- Phase 3A (registration service for auto-classification hook)

## Deliverables

- Audit store correctly wired to policy engine — every `tools/call` decision recorded in Postgres
- Postgres connection pool configured with bounded limits (max open, max idle, conn max lifetime)
- CORS restricted to explicit origin allowlist (no more wildcard `*`)
- Audit log retention with configurable TTL and background cleanup goroutine
- `tool_classifications` table with `read_only`, `write`, `destructive`, `unknown` risk levels
- Auto-classification of tools on first registration based on naming conventions
- Policy engine checks classification before all other rules — destructive tools blocked immediately
- API key `policy_profile` column so API key users get differentiated access tiers
- Tool call result audit entries (success/failure, truncated response) for compliance
- Shutdown timeout configurable and longer than heartbeat interval

## Packages Modified

- `internal/server` — audit store wiring, CORS origin checking, shutdown timeout
- `internal/config` — new env vars for DB pool, CORS origins, retention, shutdown
- `internal/store` — tool classification store, API key policy_profile migration
- `internal/policy` — classification check as first evaluation step, auto-classify logic
- `internal/registration` — hook to auto-classify tools on registration
- `internal/proxy` — tool call result audit logging
- `cmd/mcpgw` — wire new components in serve.go

## Success Criteria

- `tools/call` decisions appear in `audit_log` table with correct fields
- DB connection pool stays within configured limits under concurrent load
- Non-allowlisted CORS origin receives 403
- Audit log entries older than retention period are cleaned up automatically
- Tool registered with `delete` in its name auto-classified as `destructive`
- `tools/call` for a `destructive` tool returns 403 with reason in all policy profiles
- Admin can reclassify a tool via direct DB update (UI comes in Phase 12)
- API key created with `--profile premium` gets premium rate limits
- `go test ./...` passes with >=80% coverage on modified packages
- `go vet ./...` and `golangci-lint run` pass clean
