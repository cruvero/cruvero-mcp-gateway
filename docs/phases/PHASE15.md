# Phase 15: NATS Resilience & Security

## Goal

Wire the existing `PostgresConfigStore` into the `DegradationManager` so the gateway recovers cached config when NATS is unavailable at startup, and add TLS support for NATS connections to secure control-plane traffic in transit.

Both gaps were identified in the parity audit: Gap #1 (ConfigStore never wired — `nil` passed in `server.go:125,143`) and Gap #2 (NATS TLS config fields exist in Phase 14B spec but were never implemented).

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [15A](PHASE15A.md) | ConfigStore Wiring & Degradation Recovery | [2 prompts](PHASE15A-PROMPT.md) | server, events |
| [15B](PHASE15B.md) | NATS TLS Configuration & Connection Security | [2 prompts](PHASE15B-PROMPT.md) | config, server, events |

## Dependencies

- Phase 6 (NATS & Cruvero Integration) — provides `events.Client`, `DegradationManager`, `PostgresConfigStore`
- Phase 1B (Postgres Store & Migrations) — provides `*sql.DB` and migration infrastructure
- Phase 10A (Audit Wiring, DB Pool) — established the `SetAuditStore` wiring pattern reused here

## Deliverables

- `DegradationManager` created with a real `PostgresConfigStore` instead of `nil`
- Gateway calls `LoadCachedConfig()` on startup when NATS is unavailable, recovering last-known-good config from Postgres
- Readiness probe transitions from `disconnected` to `degraded` when cached config is available
- NATS connections encrypted with mTLS when `MCPGW_NATS_TLS_ENABLED=true`
- Config validation rejects enabled NATS TLS without cert/key/CA paths
- TLS 1.2 minimum enforced on NATS connections

## Packages Modified

- `internal/server` — accept `*sql.DB`, create `PostgresConfigStore`, pass to `NewDegradationManager`
- `internal/config` — add NATS TLS config fields and validation
- `internal/events` — `WithTLS` client option already exists; no changes needed in this package

## Success Criteria

- With NATS down and cached config in Postgres, gateway starts in `degraded` mode serving last-known config
- With NATS down and no cached config, gateway starts in `disconnected` mode (unchanged behavior)
- NATS TLS connection succeeds with valid cert/key/CA
- Config validation rejects `NATS_TLS_ENABLED=true` without all three paths
- `go test ./...` passes with >=80% coverage on modified packages
- `go vet ./...` and `golangci-lint run` pass clean
