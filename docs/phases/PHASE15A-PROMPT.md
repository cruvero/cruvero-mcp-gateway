# Phase 15A Implementation Prompts

## Prompt 1 of 2: Wire PostgresConfigStore into DegradationManager

### Required Reading (read these files before writing code)

- `docs/phases/PHASE15A.md` (full scope)
- `internal/events/persistence.go` (ConfigStore interface, PostgresConfigStore)
- `internal/events/degradation.go` (NewDegradationManager constructor, LoadCachedConfig method)
- `internal/server/server.go` (lines 54-175: `New()` function, find the two `NewDegradationManager` calls at lines 125 and 143)
- `cmd/mcpgw/serve.go` (where `*sql.DB` is created — this is the DB handle that needs to be passed to the server)

### Task

Wire the existing `PostgresConfigStore` into `DegradationManager` so cached config is loaded from Postgres when NATS is unavailable.

1. **Modify `server.New()` signature** in `internal/server/server.go`:
   - Add a `db *sql.DB` parameter: `func New(cfg *config.Config, logger *slog.Logger, db *sql.DB) *Server`
   - At the top of `New()`, create the config store (nil-safe):
     ```go
     var configStore events.ConfigStore
     if db != nil {
         configStore = events.NewPostgresConfigStore(db)
     }
     ```

2. **Replace `nil` configStore at line 125:**
   ```go
   // Before:
   degradation := events.NewDegradationManager(natsClient, nil, srv.eventSubscriber, logger)
   // After:
   degradation := events.NewDegradationManager(natsClient, configStore, srv.eventSubscriber, logger)
   ```

3. **Replace `nil` configStore at line 143:**
   ```go
   // Before:
   srv.degradation = events.NewDegradationManager(nil, nil, nil, logger)
   // After:
   srv.degradation = events.NewDegradationManager(nil, configStore, nil, logger)
   ```

4. **Add startup LoadCachedConfig** once, after **both** degradation manager code paths complete (after line 144, which is the end of the `if cfg.CruveroEnabled && srv.degradation == nil` fallback block). This single check covers both the NATS-connected and NATS-failed paths:
   ```go
   if srv.degradation != nil && !srv.degradation.EverConnected() {
       if err := srv.degradation.LoadCachedConfig(context.Background()); err != nil {
           logger.Warn("failed to load cached config from postgres", slog.String("error", err.Error()))
       } else if srv.degradation.HasCachedConfig() {
           logger.Info("loaded cached config from postgres; running in degraded mode")
       }
   }
   ```

5. **Update `cmd/mcpgw/serve.go`** to pass `db` to `server.New()`:
   - Find the call to `server.New(cfg, logger)` and change it to `server.New(cfg, logger, db)`
   - Ensure this is after `sql.Open()` and pool configuration

6. **Update all other `server.New()` call sites** (tests, other commands):
   - `internal/proxy/integration_test.go:34` — add `nil` as third parameter
   - `internal/testutil/integration_test.go:87` — add `nil` as third parameter
   - `internal/registration/integration_test.go:38` — add `nil` as third parameter
   - `internal/server/server_test.go` — add `nil` as third parameter in all `server.New()` calls

### Verification

```bash
go build ./cmd/mcpgw
go test ./internal/server/... ./internal/events/...
go vet ./...
```

### Acceptance Criteria

- `server.New()` accepts `*sql.DB` and creates `PostgresConfigStore` when non-nil
- Both `NewDegradationManager` call sites receive the config store instead of `nil`
- `LoadCachedConfig` is called at startup when NATS is not connected
- `LoadCachedConfig` errors are logged but don't prevent server startup
- Passing `nil` for `db` preserves current behavior (no config store, no panic)
- All existing tests pass without modification (or with trivial `nil` parameter addition)

---

## Prompt 2 of 2: Tests for ConfigStore Wiring

### Required Reading (read these files before writing code)

- `docs/phases/PHASE15A.md` (testing requirements)
- `internal/events/degradation.go` (all methods, especially LoadCachedConfig)
- `internal/events/persistence.go` (ConfigStore interface)
- `internal/events/degradation_test.go` (existing test patterns, if present)
- `internal/server/server_test.go` (existing test patterns)

### Task

Add tests verifying the ConfigStore wiring and LoadCachedConfig behavior.

1. **Reuse the existing `mockConfigStore`** from `internal/events/subscriber_test.go` (line 260). It is already used in `degradation_test.go`. The existing mock has:
   - `keys []string` and `values map[string][]byte` for data
   - `keysErr error` for simulating `Keys()` failures
   - `loadErrs map[string]error` for per-key `Load()` errors (returns `sql.ErrNoRows` for missing keys)
   - `Save()` is a no-op (returns nil)

   Since the mock is in `subscriber_test.go` (same package), it is accessible from `degradation_test.go`. Do NOT create a duplicate mock.

2. **Test: DegradationManager with config store loads cached config:**
   ```go
   func TestDegradationManager_LoadCachedConfig_WithStore(t *testing.T) {
       store := &mockConfigStore{
           keys:   []string{"server_config"},
           values: map[string][]byte{"server_config": []byte(`{"version": 1}`)},
       }
       dm := NewDegradationManager(nil, store, nil, nil)

       err := dm.LoadCachedConfig(context.Background())
       require.NoError(t, err)
       assert.True(t, dm.HasCachedConfig())
       assert.Equal(t, DegradationStatusDegraded, dm.Status()) // disconnected + cached = degraded
   }
   ```

3. **Test: DegradationManager with nil config store returns nil (no-op):**
   ```go
   func TestDegradationManager_LoadCachedConfig_NilStore(t *testing.T) {
       dm := NewDegradationManager(nil, nil, nil, nil)

       err := dm.LoadCachedConfig(context.Background())
       require.NoError(t, err) // nil store returns nil, not error
       assert.False(t, dm.HasCachedConfig())
       assert.Equal(t, DegradationStatusDisconnected, dm.Status())
   }
   ```

4. **Test: DegradationManager with empty config cache:**
   ```go
   func TestDegradationManager_LoadCachedConfig_EmptyCache(t *testing.T) {
       store := &mockConfigStore{
           keys:   []string{},
           values: map[string][]byte{},
       }
       dm := NewDegradationManager(nil, store, nil, nil)

       err := dm.LoadCachedConfig(context.Background())
       require.NoError(t, err)
       assert.False(t, dm.HasCachedConfig())
       assert.Equal(t, DegradationStatusDisconnected, dm.Status())
   }
   ```

5. **Test: LoadCachedConfig error is propagated:**
   ```go
   func TestDegradationManager_LoadCachedConfig_StoreError(t *testing.T) {
       store := &mockConfigStore{
           keysErr: fmt.Errorf("connection refused"),
       }
       dm := NewDegradationManager(nil, store, nil, nil)

       err := dm.LoadCachedConfig(context.Background())
       require.Error(t, err)
       assert.Contains(t, err.Error(), "connection refused")
   }
   ```

6. **Test: nil receiver is safe:**
   ```go
   func TestDegradationManager_LoadCachedConfig_NilReceiver(t *testing.T) {
       var dm *DegradationManager
       err := dm.LoadCachedConfig(context.Background())
       require.NoError(t, err) // nil receiver returns nil
   }
   ```

### Verification

```bash
go test -v -race ./internal/events/...
go test -v -race ./internal/server/...
go test -coverprofile=cover.out ./internal/events/... && go tool cover -func=cover.out | grep -E 'degradation|persistence'
```

### Acceptance Criteria

- All 5+ test cases pass
- `LoadCachedConfig` with mock store verifies status transitions
- Nil config store is confirmed safe (no panic)
- Empty cache is handled correctly (no status change)
- Store errors propagate to the caller
- Coverage on `internal/events/degradation.go` is >=80%
