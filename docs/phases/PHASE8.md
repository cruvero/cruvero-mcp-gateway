# Phase 8: CLI & Testing

## Goal

Build the admin CLI for gateway management and implement comprehensive integration tests, load tests, and security tests to achieve 80%+ coverage across all packages.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [8A](PHASE8A.md) | Admin CLI | [3 prompts](PHASE8A-PROMPT.md) | cmd/mcpgw |
| [8B](PHASE8B.md) | Integration Tests, Load Tests, Coverage | [3 prompts](PHASE8B-PROMPT.md) | testutil, all packages |

## Dependencies

- Phase 1--7 (all previous phases must be complete)

## Deliverables

- `mcpgw` binary with subcommands: serve, server, apikey, policy, health, migrate
- Integration tests covering full lifecycle flows
- Load tests for rate limiting and HPA behavior
- Security tests for cert validation and auth bypass attempts
- Coverage gate: 80% minimum per package
- Coverage check script

## Packages Created

- `internal/testutil` -- Test helpers, fixtures, mock servers

## Success Criteria

- All CLI subcommands work correctly
- Integration tests pass with real (test) Postgres and NATS
- Load tests validate rate limiting under concurrent load
- Security tests verify auth boundary enforcement
- Every package at >=80% coverage
- `./scripts/check-coverage.sh` passes in CI
