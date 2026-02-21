# Phase 6B Implementation Prompts

## Prompt 1 of 3: Config Subscriber & Handlers

### Required Reading (read these files before writing code)
- docs/phases/PHASE6B.md
- internal/events/client.go
- internal/events/types.go
- internal/policy/engine.go
- internal/ratelimit/store.go
- internal/registration/service.go

### Task

Create the config subscription system and handlers.

1. `internal/events/subscriber.go`:
   - Define ConfigHandler interface: Handle(ctx context.Context, data []byte) error
   - Define Subscriber struct: client, handlers map[string]ConfigHandler, subscriptions []*nats.Subscription, logger
   - NewSubscriber(client *Client, logger *slog.Logger) *Subscriber
   - RegisterHandler(subject string, handler ConfigHandler): add to handlers map
   - Register gateway-scoped subjects (`mcpgw.{gateway_id}.config.policy`, `.servers`, `.server_settings`, `.auth`)
   - Start(ctx context.Context) error: subscribe to all registered subjects, store subscriptions
   - Stop() error: unsubscribe all, drain
   - Message routing: on NATS message, look up handler by subject, call Handle

2. `internal/events/handlers.go`:
   - Define config message types: PolicyConfigMessage, ServerConfigMessage, ServerSettingsConfigMessage, AuthConfigMessage (JSON)
   - PolicyConfigHandler struct: engine *policy.Engine, limiterStore *ratelimit.LimiterStore, logger
   - Handle: unmarshal PolicyConfigMessage, validate, update engine profiles, update limiter defaults
   - ServerConfigHandler struct: registrationService, logger
   - Handle: unmarshal ServerConfigMessage, validate, update allowed SPIFFE prefixes
   - ServerSettingsConfigHandler struct: registrationService/configStore/logger
   - Handle: unmarshal ServerSettingsConfigMessage, validate non-secret settings schema, version payload, persist last-known-good effective settings
   - AuthConfigHandler struct: (placeholder for future OIDC config updates)

3. Tests:
   - `internal/events/subscriber_test.go`: Test handler registration, test message routing, test Start/Stop lifecycle
   - `internal/events/handlers_test.go`: Test PolicyConfigHandler with valid and invalid config, test ServerConfigHandler, test ServerSettingsConfigHandler valid/versioned/invalid paths

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Subscriber routes messages to correct handlers
- Handlers validate before applying
- Clean Start/Stop lifecycle

---

## Prompt 2 of 3: Config Persistence

### Required Reading (read these files before writing code)
- docs/phases/PHASE6B.md
- internal/events/handlers.go
- internal/store/interfaces.go

### Task

Implement config persistence for last-known-good config.

1. `internal/events/persistence.go`:
   - Define ConfigStore interface: Save(ctx, key string, value []byte) error, Load(ctx, key string) ([]byte, error), Keys(ctx) ([]string, error)
   - Define PostgresConfigStore struct: db *sql.DB
   - NewPostgresConfigStore(db *sql.DB) *PostgresConfigStore
   - Save: UPSERT into config_cache table (key text PK, value bytea, updated_at timestamptz)
   - Load: SELECT value FROM config_cache WHERE key = $1
   - Keys: SELECT key FROM config_cache
   - Note: migration for config_cache table (0004_config_cache.up.sql)
   - Add migration file reference to PHASE6B deliverables

2. Update handlers to persist on update:
   - After successfully applying config, save to ConfigStore
   - On startup, load from ConfigStore and apply as initial config
   - For server settings, persist versioned effective settings snapshots used by registration/heartbeat responses

3. `internal/events/persistence_test.go`:
   - Test Save and Load round trip (sqlmock)
   - Test Load for non-existent key returns error
   - Test UPSERT behavior (save twice, load gets latest)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Config persisted to Postgres on every update
- Startup loads last-known config
- Clean error handling for missing config

---

## Prompt 3 of 3: Graceful Degradation & Health

### Required Reading (read these files before writing code)
- docs/phases/PHASE6B.md
- internal/events/client.go
- internal/events/persistence.go
- internal/server/server.go

### Task

Implement graceful degradation and health reporting.

1. `internal/events/degradation.go`:
   - Define DegradationManager struct: client *Client, configStore ConfigStore, subscriber *Subscriber, logger
   - NewDegradationManager constructor
   - OnDisconnect(): log warning, set degraded state
   - OnReconnect(ctx context.Context):
     - Request full config snapshot: publish to "mcpgw.{gateway_id}.config.request" with gateway_id and last-known versions
     - Log recovery
   - LoadCachedConfig(ctx context.Context) error: load all keys from ConfigStore, apply via handlers
   - Status() DegradationStatus: Connected/Degraded/Disconnected

2. Wire into NATS client callbacks:
   - On disconnect -> degradationManager.OnDisconnect()
   - On reconnect -> degradationManager.OnReconnect()

3. Update health endpoints:
   - /healthz: always 200 if process running
   - /readyz: include NATS status in response
     - If MCPGW_CRUVERO_ENABLED=true and never connected and no cache: 503
     - If connected or has cached config: 200 with status detail
   - Response body includes: {"status":"ok","nats":"connected"} or {"status":"degraded","nats":"disconnected"}
   - Include settings sync fields when available: `settings_sync_status`, `settings_config_version`

4. Tests:
   - `internal/events/degradation_test.go`: Test OnDisconnect sets state, test OnReconnect triggers snapshot request, test LoadCachedConfig
   - Test health endpoints reflect NATS status and settings sync fields

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Gateway continues operating on NATS disconnect
- Reconnection triggers config sync
- Health endpoints accurately reflect state
- >=80% coverage across events package
