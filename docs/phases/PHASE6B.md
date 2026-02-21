# Phase 6B: Config Sync from Cruvero, Graceful Degradation

## Overview

Implement the config subscription system where the gateway receives policy, server, server-settings, and auth configuration updates from Cruvero via NATS. The gateway gracefully degrades when NATS is unavailable and requests a full config snapshot on reconnection.

## Scope

### Config Subscriber (`internal/events/subscriber.go`)
- `Subscriber` struct: client *Client, handlers map[string]ConfigHandler, logger *slog.Logger
- `ConfigHandler` interface: `Handle(ctx context.Context, data []byte) error`
- Subscribe to subjects:
  - `mcpgw.{gateway_id}.config.policy` — policy profile updates
  - `mcpgw.{gateway_id}.config.servers` — server allowlist/denylist updates
  - `mcpgw.{gateway_id}.config.server_settings` — non-secret runtime settings updates for backend MCP servers
  - `mcpgw.{gateway_id}.config.auth` — auth configuration updates (API key policies, OIDC settings)
- `Start(ctx context.Context) error`: subscribe to all config subjects
- `Stop() error`: unsubscribe all

### Config Handlers (`internal/events/handlers.go`)
- `PolicyConfigHandler`: receives PolicyProfile updates, validates, applies to policy Engine and ratelimit store
- `ServerConfigHandler`: receives server allowlist updates, validates, applies to registration Service
- `ServerSettingsConfigHandler`: receives non-secret backend settings, validates schema/types, versions payloads, persists last-known-good, and exposes effective settings for registration/heartbeat responses
- `AuthConfigHandler`: receives auth config updates, validates, applies to auth layer
- Each handler:
  1. Unmarshal incoming JSON
  2. Validate structure and values
  3. Apply to in-memory state
  4. Persist to local Postgres (last-known-good config)
  5. Log the update

### Server Settings Apply Contract
- Source of truth in gateway mode: Cruvero-provided effective non-secret settings.
- Gateway attaches `config_version` and `effective_settings` to registration and heartbeat responses.
- Backend servers apply only hot-reload-safe keys on next heartbeat cycle.
- Invalid settings are rejected; backend keeps last-known-good settings and reports status.

### Config Persistence (`internal/events/persistence.go`)
- `ConfigStore` interface: Save(ctx context.Context, key string, value []byte) error, Load(ctx context.Context, key string) ([]byte, error)
- `PostgresConfigStore` implementation: uses a config_cache table (or reuses existing store)
- On startup: load last-known config from Postgres
- On config update: save to Postgres
- On NATS reconnect: request full snapshot, compare with local, apply deltas
- Persist versioned server-settings snapshots for deterministic merge/serve behavior

### Graceful Degradation (`internal/events/degradation.go`)
- When NATS disconnects:
  - Log warning
  - Continue operating with last-known config (from Postgres or in-memory)
  - Health endpoint reflects degraded state (NATS disconnected)
  - No config changes possible until reconnection
- When NATS reconnects:
  - Request full config snapshot from Cruvero: publish to `mcpgw.{gateway_id}.config.request` with gateway_id
  - Apply received config (overrides local)
  - Log recovery

### Health Integration
- Update /readyz to include NATS status:
  - Connected: fully ready
  - Disconnected but has cached config: ready (degraded)
  - Never connected and no cached config: not ready (if Cruvero integration enabled)
- Include settings-sync detail in readiness response (`settings_sync_status`, `settings_config_version` when available)

## Files Created

| File | Description |
|------|-------------|
| internal/events/subscriber.go | Config subscription manager |
| internal/events/handlers.go | Config update handlers |
| internal/events/persistence.go | Config persistence to Postgres |
| internal/events/degradation.go | Graceful degradation logic |
| migrations/0004_config_cache.up.sql | Config cache table for last-known-good snapshots |
| internal/events/subscriber_test.go | Subscriber tests |
| internal/events/handlers_test.go | Handler tests |
| internal/events/persistence_test.go | Persistence tests |
| internal/events/degradation_test.go | Degradation tests |

## Testing Requirements

- Subscriber: test subscription setup, test message routing to correct handler
- Handlers: test each handler applies config correctly, test validation rejects bad config
- Server settings: test versioned apply path, test invalid payload rejected, test last-known-good retained
- Persistence: test save/load round trip, test load on startup
- Degradation: test continued operation on disconnect, test config snapshot request on reconnect
- Health: test ready states for connected/disconnected/never-connected
- Coverage: >=80%
