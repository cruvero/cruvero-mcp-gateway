# Phase 10A Implementation Prompts

## Prompt 1 of 4: Audit Store Wiring & DB Connection Pool

### Required Reading (read these files before writing code)
- docs/phases/PHASE10A.md (full scope)
- internal/server/server.go (find where policy engine is created with nil audit store)
- internal/policy/engine.go (Engine struct, audit store field)
- internal/policy/audit.go (LogDecision nil check)
- cmd/mcpgw/serve.go (where audit store is created, where server is created)
- internal/config/config.go (existing config pattern)

### Task

Fix the silently broken audit trail and configure DB connection pooling.

1. **Wire audit store to policy engine:**
   - Add `SetAuditStore(as store.AuditStore)` method to `*policy.Engine` that sets `e.auditStore = as`.
   - Add `SetAuditStore(as store.AuditStore)` method to `*Server` in `internal/server/server.go` that calls `s.policyEngine.SetAuditStore(as)`.
   - In `cmd/mcpgw/serve.go`, after the audit store is created (around line 78) and after the server is created, call `srv.SetAuditStore(auditStore)`.
   - **Do NOT change the `NewEngine()` constructor signature** — this avoids breaking existing call sites. The setter pattern allows wiring after construction.

2. **DB connection pool config:**
   - In `internal/config/config.go`, add to the Config struct:
     ```go
     DBMaxOpenConns    int           `json:"db_max_open_conns"`
     DBMaxIdleConns    int           `json:"db_max_idle_conns"`
     DBConnMaxLifetime time.Duration `json:"db_conn_max_lifetime"`
     ```
   - In `Load()`, parse:
     - `MCPGW_DB_MAX_OPEN_CONNS` (default `25`)
     - `MCPGW_DB_MAX_IDLE_CONNS` (default `10`)
     - `MCPGW_DB_CONN_MAX_LIFETIME` (default `"5m"`)
   - In `Validate()`:
     - `DBMaxOpenConns` must be > 0
     - `DBMaxIdleConns` must be > 0 and <= `DBMaxOpenConns`
     - `DBConnMaxLifetime` must be > 0
   - In `cmd/mcpgw/serve.go`, after `sql.Open()`:
     ```go
     db.SetMaxOpenConns(cfg.DBMaxOpenConns)
     db.SetMaxIdleConns(cfg.DBMaxIdleConns)
     db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
     ```

3. **Tests:**
   - `internal/policy/engine_test.go`: Add test that `SetAuditStore` replaces nil store and subsequent `Evaluate()` calls produce audit entries (use an in-memory mock audit store from testutil).
   - `internal/config/config_test.go`: Add tests for new DB pool fields — defaults, custom values, validation failures (idle > open, zero values, negative values).

### Verification
```bash
go test ./internal/policy/... ./internal/config/... ./internal/server/...
go vet ./...
```

### Acceptance Criteria
- `policy.Engine.Evaluate()` produces audit log entries when audit store is wired
- `policy.Engine.Evaluate()` still works (no panic) when audit store is nil (backwards compatible)
- DB pool config fields parse correctly with defaults
- Validation catches invalid pool configurations
- No changes to `NewEngine()` constructor signature

---

## Prompt 2 of 4: CORS Origin Allowlist

### Required Reading (read these files before writing code)
- docs/phases/PHASE10A.md (P0-3 section)
- internal/server/middleware.go (find the CORS handler, line ~104)
- internal/config/config.go (existing CORS fields)

### Task

Replace the wildcard CORS origin with an explicit allowlist.

1. **Config changes** in `internal/config/config.go`:
   - Add `CORSAllowedOrigins []string` field.
   - In `Load()`, parse `MCPGW_CORS_ALLOWED_ORIGINS` as comma-separated, trim whitespace from each entry.
   - In `Validate()`: if `CORSEnabled` is true, `CORSAllowedOrigins` must be non-empty.

2. **Middleware changes** in `internal/server/middleware.go`:
   - Replace the existing CORS handler. Remove `Access-Control-Allow-Origin: *`.
   - Create a new CORS middleware function: `CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler`.
   - Implementation:
     ```go
     func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
         // Build a set for O(1) lookup. Normalize to lowercase.
         originSet := make(map[string]bool, len(allowedOrigins))
         for _, o := range allowedOrigins {
             originSet[strings.ToLower(strings.TrimSpace(o))] = true
         }
         
         return func(next http.Handler) http.Handler {
             return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                 origin := r.Header.Get("Origin")
                 w.Header().Set("Vary", "Origin") // Always set to prevent cache poisoning
                 
                 if origin != "" && originSet[strings.ToLower(origin)] {
                     w.Header().Set("Access-Control-Allow-Origin", origin) // Echo matched origin, not *
                     w.Header().Set("Access-Control-Allow-Credentials", "true")
                     w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                     w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
                     w.Header().Set("Access-Control-Max-Age", "86400")
                 }
                 
                 if r.Method == http.MethodOptions {
                     w.WriteHeader(http.StatusNoContent)
                     return
                 }
                 
                 next.ServeHTTP(w, r)
             })
         }
     }
     ```
   - Wire this middleware in the server's middleware chain, replacing the old CORS logic.

3. **Tests** in `internal/server/middleware_test.go`:
   - Test: allowlisted origin gets correct `Access-Control-Allow-Origin` header (echoed, not `*`).
   - Test: non-allowlisted origin gets no `Access-Control-Allow-Origin` header.
   - Test: `Vary: Origin` is always present regardless of origin match.
   - Test: OPTIONS preflight returns 204 with correct headers for allowlisted origin.
   - Test: OPTIONS preflight for non-allowlisted origin returns 204 but without ACAO header.
   - Test: case-insensitive origin matching (e.g., `https://App.Example.COM` matches `https://app.example.com`).
   - Test: `Access-Control-Allow-Credentials: true` is set when origin matches.

### Verification
```bash
go test ./internal/server/... ./internal/config/...
go vet ./...
```

### Acceptance Criteria
- No request ever receives `Access-Control-Allow-Origin: *`
- Allowlisted origin receives the echoed origin value
- Non-allowlisted origin receives no ACAO header
- `Vary: Origin` always present
- Config validation rejects `CORSEnabled=true` without origins

---

## Prompt 3 of 4: Audit Log Retention & Shutdown Timeout

### Required Reading (read these files before writing code)
- docs/phases/PHASE10A.md (P0-4 and P1-3 sections)
- internal/store/interfaces.go (existing store pattern)
- migrations/ (existing migration file naming)
- internal/server/server.go (shutdown timeout constant)

### Task

Add background audit log retention and configurable shutdown timeout.

1. **Migration** `migrations/0005_audit_log_retention_index.up.sql`:
   ```sql
   CREATE INDEX IF NOT EXISTS idx_audit_log_created_at ON audit_log(created_at);
   ```
   Down migration `migrations/0005_audit_log_retention_index.down.sql`:
   ```sql
   DROP INDEX IF EXISTS idx_audit_log_created_at;
   ```

2. **Retention goroutine** `internal/store/retention.go`:
   ```go
   // StartAuditRetention runs a background goroutine that deletes audit_log entries
   // older than retentionDays. It runs every interval and stops on context cancellation.
   func StartAuditRetention(ctx context.Context, db *sql.DB, retentionDays int, interval time.Duration, logger *slog.Logger) {
       ticker := time.NewTicker(interval)
       defer ticker.Stop()
       for {
           select {
           case <-ctx.Done():
               logger.Info("audit retention goroutine stopped")
               return
           case <-ticker.C:
               deleted, err := cleanupAuditLog(ctx, db, retentionDays)
               if err != nil {
                   logger.Error("audit retention cleanup failed", "error", err)
                   continue
               }
               if deleted > 0 {
                   logger.Info("audit retention cleanup completed", "deleted_rows", deleted)
               }
           }
       }
   }
   
   func cleanupAuditLog(ctx context.Context, db *sql.DB, retentionDays int) (int64, error) {
       result, err := db.ExecContext(ctx,
           "DELETE FROM audit_log WHERE created_at < now() - make_interval(days => $1)",
           retentionDays,
       )
       if err != nil {
           return 0, fmt.Errorf("audit retention delete: %w", err)
       }
       return result.RowsAffected()
   }
   ```

3. **Config additions** in `internal/config/config.go`:
   - Add `AuditRetentionDays int` (default `90`), parse `MCPGW_AUDIT_RETENTION_DAYS`.
   - Add `AuditCleanupInterval time.Duration` (default `1h`), parse `MCPGW_AUDIT_CLEANUP_INTERVAL`.
   - Add `ShutdownTimeout time.Duration` (default `30s`), parse `MCPGW_SHUTDOWN_TIMEOUT`.
   - Validate: `AuditRetentionDays > 0`, `AuditCleanupInterval > 0`, `ShutdownTimeout > 0 && ShutdownTimeout <= 120s`.

4. **Shutdown timeout** in `internal/server/server.go`:
   - Remove hardcoded `shutdownTimeout = 10 * time.Second`.
   - Accept shutdown timeout from config (add parameter to `New()` or accept via config struct).
   - Use the configured value in the graceful shutdown context deadline.

5. **Wiring** in `cmd/mcpgw/serve.go`:
   - After DB and config are ready, start the retention goroutine:
     ```go
     go store.StartAuditRetention(ctx, db, cfg.AuditRetentionDays, cfg.AuditCleanupInterval, logger)
     ```
   - Pass `cfg.ShutdownTimeout` to the server constructor or config.

6. **Tests:**
   - `internal/store/retention_test.go`:
     - Use a test Postgres DB (or sqlmock).
     - Insert audit entries with various `created_at` timestamps.
     - Call `cleanupAuditLog()` with retention of 30 days.
     - Verify entries older than 30 days are deleted, recent entries preserved.
     - Test with 0 rows to delete (no error).
   - `internal/config/config_test.go`: Tests for retention and shutdown config fields.

### Verification
```bash
go test ./internal/store/... ./internal/config/... ./internal/server/...
go vet ./...
```

### Acceptance Criteria
- Index migration applies cleanly
- Retention goroutine deletes old entries and preserves recent ones
- Retention goroutine stops on context cancellation (no goroutine leak)
- Shutdown timeout is configurable and respected
- Config validation catches invalid values

---

## Prompt 4 of 4: API Key Policy Profile & Tool Call Result Audit

### Required Reading (read these files before writing code)
- docs/phases/PHASE10A.md (P1-1 and P1-2 sections)
- internal/store/apikey_store.go (existing API key CRUD)
- internal/auth/apikey_middleware.go (where identity metadata is set)
- internal/proxy/server.go (tool handler closure, around line 264-286)
- cmd/mcpgw/apikey.go (existing CLI command)

### Task

Add policy profile support to API keys and audit tool call results.

1. **Migration** `migrations/0006_apikey_policy_profile.up.sql`:
   ```sql
   ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS policy_profile TEXT NOT NULL DEFAULT 'default';
   ```
   Down migration `migrations/0006_apikey_policy_profile.down.sql`:
   ```sql
   ALTER TABLE api_keys DROP COLUMN IF EXISTS policy_profile;
   ```

2. **API key store** `internal/store/apikey_store.go`:
   - Add `PolicyProfile string` field to the API key struct (if not a separate type, add it to the relevant struct used by `Create` and `GetByLookupHash`).
   - Update `Create()` INSERT statement to include `policy_profile`.
   - Update `GetByLookupHash()` SELECT to include `policy_profile`.
   - Update any scan functions to handle the new column.

3. **API key middleware** `internal/auth/apikey_middleware.go`:
   - After successfully looking up and verifying the key (around line 63-65), add:
     ```go
     id.Metadata["policy_profile"] = apiKey.PolicyProfile
     ```
   - Ensure `id.Metadata` is initialized (not nil) before setting.

4. **CLI** `cmd/mcpgw/apikey.go`:
   - Add `--profile` flag to `mcpgw apikey create` (default `"default"`).
   - Pass the profile value to the API key store's `Create()` method.
   - Show the profile in `mcpgw apikey list` output.

5. **Tool call result audit** `internal/proxy/server.go`:
   - Thread audit store into `ProxyServer`. Add it as a field set via constructor or setter method.
   - In the tool handler closure, after `p.router.Route()` returns (successful or error):
     ```go
     // Log tool call result
     resultEntry := &types.AuditEntry{
         EventType: "tool_call_result",
         ClientID:  clientID,
         ToolName:  toolName,
         Metadata: map[string]string{
             "success":       strconv.FormatBool(err == nil),
             "duration_ms":   strconv.FormatInt(duration.Milliseconds(), 10),
         },
     }
     if err != nil {
         resultEntry.Metadata["error"] = truncate(err.Error(), 1024)
     } else {
         resultEntry.Metadata["response_preview"] = truncate(responseBody, 1024)
     }
     // Fire-and-forget audit (don't block the response)
     go func() {
         if auditErr := p.auditStore.Create(context.Background(), resultEntry); auditErr != nil {
             p.logger.Error("failed to audit tool call result", "error", auditErr)
         }
     }()
     ```
   - Add a `truncate(s string, maxLen int) string` helper function.
   - Wire audit store into ProxyServer from `cmd/mcpgw/serve.go`.

6. **Tests:**
   - `internal/store/apikey_store_test.go`: Test creating key with custom profile, verify GetByLookupHash returns profile.
   - `internal/auth/apikey_middleware_test.go`: Test that middleware sets `policy_profile` in identity metadata.
   - `internal/proxy/server_test.go`: Test that tool call result is audited on success and on failure.
   - `cmd/mcpgw/apikey_test.go`: Test `--profile` flag parsing.

### Verification
```bash
go test ./internal/store/... ./internal/auth/... ./internal/proxy/... ./cmd/mcpgw/...
go vet ./...
```

### Acceptance Criteria
- API key created with `--profile premium` stores and returns the profile
- Middleware sets `policy_profile` metadata on identity
- Policy middleware reads the profile and applies correct rate limits
- Successful tool calls produce `tool_call_result` audit entries with duration
- Failed tool calls produce audit entries with error message
- Response preview is truncated to 1024 bytes
- Audit is fire-and-forget (doesn't slow down the response)
