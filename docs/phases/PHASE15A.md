# Phase 15A: ConfigStore Wiring & Degradation Recovery

## Overview

Wire the existing `PostgresConfigStore` implementation into `DegradationManager` so the gateway can recover cached control-plane config from Postgres when NATS is unavailable at startup or after disconnection.

## Scope

### Wire PostgresConfigStore into DegradationManager (`internal/server/server.go`)

The `DegradationManager` constructor accepts a `ConfigStore` interface parameter, but `server.go` passes `nil` in both call sites:

- **Line 125**: `degradation := events.NewDegradationManager(natsClient, nil, srv.eventSubscriber, logger)` — when NATS connects successfully
- **Line 143**: `srv.degradation = events.NewDegradationManager(nil, nil, nil, logger)` — fallback when Cruvero is enabled but NATS client creation fails

The `PostgresConfigStore` (in `internal/events/persistence.go`) is fully implemented with `Save`, `Load`, and `Keys` methods backed by the `config_cache` table, but it is never instantiated in the server lifecycle.

**Requirements:**
- Accept `*sql.DB` in `server.New()` (add parameter or pass via `Config` struct)
- Create `events.NewPostgresConfigStore(db)` when `db` is non-nil
- Pass the store to both `NewDegradationManager` call sites instead of `nil`
- After creating the degradation manager, call `degradation.LoadCachedConfig(ctx)` when NATS is not connected
- Handle `LoadCachedConfig` errors gracefully — log warning, continue startup

### LoadCachedConfig Startup Behavior

When `LoadCachedConfig` succeeds and finds cached keys:
- `DegradationManager.status` transitions from `disconnected` to `degraded` (line 165-167 of `degradation.go`)
- `DegradationManager.hasCachedConfig` becomes `true`
- The subscriber's `LoadCachedConfig` applies the cached policy/server config

This means the readiness probe will report `degraded` instead of `disconnected`, enabling the pod to serve traffic with stale-but-valid config while NATS recovers.

### Nil-Safety Preservation

The existing nil-safety in `DegradationManager` methods must be preserved:
- `LoadCachedConfig` returns `nil` when `configStore` is `nil` (line 127-129)
- All methods return safe defaults when receiver is `nil`
- No changes needed to the `DegradationManager` or `PostgresConfigStore` implementations

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/server/server.go` | Accept `*sql.DB`, create `PostgresConfigStore`, pass to both `NewDegradationManager` calls, call `LoadCachedConfig` |
| `internal/server/server_test.go` | Test ConfigStore wiring and LoadCachedConfig behavior |

## Testing Requirements

- Mock `ConfigStore` interface, verify `DegradationManager` calls `Load`/`Keys` on cached config load
- Test: nil DB → `nil` config store passed → no panic, same behavior as before
- Test: valid DB + cached config in Postgres → `LoadCachedConfig` succeeds, status transitions to `degraded`
- Test: valid DB + empty config cache → `LoadCachedConfig` succeeds, status stays `disconnected`
- Test: `LoadCachedConfig` error is logged but doesn't prevent server startup
- Coverage: >=80% on `internal/server`
