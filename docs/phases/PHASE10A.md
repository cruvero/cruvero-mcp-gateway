# Phase 10A: P0 Bug Fixes — Audit Wiring, DB Pool, CORS, Retention

## Overview

Fix the four P0 blockers identified in the production readiness assessment. These are all prerequisite to any production traffic. Additionally fix three P1 items (API key profiles, tool call result audit, shutdown timeout) that are tightly coupled to the P0 audit and config changes.

## Scope

### P0-1: Wire Audit Store to Policy Engine (`internal/server/server.go`, `cmd/mcpgw/serve.go`)

The policy engine is created with `nil` as the audit store in `server.go:83`:
```go
policyEngine := policy.NewEngine(profiles, nil, logger)
```

The audit store IS created in `serve.go:78` but only passed to the registration service. Since `LogDecision()` in `policy/audit.go:19` returns nil when auditStore is nil, every policy decision is silently discarded.

- Add `SetAuditStore(store store.AuditStore)` method to `*Server` that threads the store into the policy engine.
- Add `SetAuditStore(store store.AuditStore)` method to `*policy.Engine`.
- Call `server.SetAuditStore(auditStore)` from `serve.go` after creating the audit store.
- Verify: make a `tools/call`, confirm a `policy_decision` row appears in `audit_log`.

### P0-2: Postgres Connection Pool Limits (`internal/config/config.go`, `cmd/mcpgw/serve.go`)

`serve.go:136-151` calls `sql.Open()` but never sets pool limits. Go defaults: unlimited open conns, 2 idle conns, no lifetime. With HPA at 20 replicas, this will exhaust Postgres `max_connections`.

- Add config fields:
  - `MCPGW_DB_MAX_OPEN_CONNS` (default 25)
  - `MCPGW_DB_MAX_IDLE_CONNS` (default 10)
  - `MCPGW_DB_CONN_MAX_LIFETIME` (default `5m`, parsed as `time.Duration`)
- After `sql.Open()` in `serve.go`, call:
  ```go
  db.SetMaxOpenConns(cfg.DBMaxOpenConns)
  db.SetMaxIdleConns(cfg.DBMaxIdleConns)
  db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
  ```
- Validate: `DBMaxOpenConns > 0`, `DBMaxIdleConns > 0`, `DBMaxIdleConns <= DBMaxOpenConns`.

### P0-3: CORS Origin Allowlist (`internal/config/config.go`, `internal/server/middleware.go`)

`middleware.go:104` sets `Access-Control-Allow-Origin: *` when CORS is enabled. With bearer tokens, this is a credential-leaking vector.

- Add config field: `MCPGW_CORS_ALLOWED_ORIGINS` (comma-separated list of allowed origins, required when CORS enabled).
- Replace wildcard logic in middleware with:
  1. Parse request `Origin` header.
  2. Check against allowlist (exact match, case-insensitive scheme+host+port).
  3. If match: set `Access-Control-Allow-Origin` to the matched origin (not `*`).
  4. If no match: omit the header entirely (browser will block the request).
  5. Always set `Vary: Origin` to prevent CDN cache poisoning.
- Add `Access-Control-Allow-Credentials: true` when origin matches.
- Validate: `CORSEnabled=true` requires non-empty `CORSAllowedOrigins`.

### P0-4: Audit Log Retention (`internal/store/retention.go`, `migrations/`)

The `audit_log` table grows unboundedly. Under production load, disk exhaustion is inevitable.

- Add config fields:
  - `MCPGW_AUDIT_RETENTION_DAYS` (default 90)
  - `MCPGW_AUDIT_CLEANUP_INTERVAL` (default `1h`, parsed as `time.Duration`)
- Create `internal/store/retention.go`:
  - `StartAuditRetention(ctx context.Context, db *sql.DB, retentionDays int, interval time.Duration, logger *slog.Logger)`
  - Background goroutine: `DELETE FROM audit_log WHERE created_at < now() - interval '$1 days'` with parameterized days.
  - Log the number of rows deleted each cycle.
  - Stop on context cancellation.
- Add migration `migrations/0005_audit_log_retention_index.up.sql`:
  ```sql
  CREATE INDEX idx_audit_log_created_at ON audit_log(created_at);
  ```
  This index makes the retention DELETE efficient.
- Down migration drops the index.

### P1-1: API Key Policy Profile (`migrations/`, `internal/store/apikey_store.go`, `internal/auth/apikey_middleware.go`, `cmd/mcpgw/apikey.go`)

API key users always get the `default` policy profile. Add a `policy_profile` column.

- New migration `migrations/0006_apikey_policy_profile.up.sql`:
  ```sql
  ALTER TABLE api_keys ADD COLUMN policy_profile TEXT NOT NULL DEFAULT 'default';
  ```
- Update `APIKeyStore.Create()` and `GetByLookupHash()` to include `policy_profile`.
- In `apikey_middleware.go:63-65`, set `id.Metadata["policy_profile"]` from the key record.
- Add `--profile` flag to `mcpgw apikey create` command.

### P1-2: Tool Call Result Audit (`internal/proxy/server.go`)

Even with P0-1 fixed, the audit only logs the policy *decision*. It does not log whether the tool call succeeded or failed.

- In the tool handler closure in `proxy/server.go` (after `p.router.Route()` returns), log a new audit entry:
  - `event_type`: `tool_call_result`
  - Fields: `tool_name`, `client_id`, `success` (bool), `error_message` (if failed), `response_truncated` (first 1024 bytes of response)
  - Duration of the tool call.
- Thread audit store into `ProxyServer` via constructor or setter.

### P1-3: Shutdown Timeout (`internal/config/config.go`, `internal/server/server.go`)

Shutdown timeout (10s) is shorter than MCP heartbeat interval (15s). Active IDE sessions get hard-cut during deploys.

- Add config field: `MCPGW_SHUTDOWN_TIMEOUT` (default `30s`, parsed as `time.Duration`).
- Replace hardcoded `shutdownTimeout = 10 * time.Second` with config value.
- Validate: shutdown timeout must be > 0 and <= 120s.
- Document: Kubernetes `terminationGracePeriodSeconds` must be >= `MCPGW_SHUTDOWN_TIMEOUT` + 5s buffer.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/server/server.go` | Add `SetAuditStore()`, use config shutdown timeout |
| `internal/server/middleware.go` | CORS origin allowlist checking with `Vary: Origin` |
| `internal/config/config.go` | Add DB pool, CORS, retention, shutdown config fields |
| `internal/config/config_test.go` | Tests for new config fields and validation |
| `internal/policy/engine.go` | Add `SetAuditStore()` method |
| `internal/store/retention.go` | Background audit log cleanup goroutine |
| `internal/store/retention_test.go` | Tests for retention logic |
| `internal/store/apikey_store.go` | Add `policy_profile` to API key CRUD |
| `internal/auth/apikey_middleware.go` | Set `policy_profile` in identity metadata |
| `internal/proxy/server.go` | Add tool call result audit logging |
| `cmd/mcpgw/serve.go` | Wire audit store, DB pool settings, retention goroutine |
| `cmd/mcpgw/apikey.go` | Add `--profile` flag |
| `migrations/0005_audit_log_retention_index.up.sql` | Index for efficient retention cleanup |
| `migrations/0005_audit_log_retention_index.down.sql` | Drop index |
| `migrations/0006_apikey_policy_profile.up.sql` | Add `policy_profile` column to `api_keys` |
| `migrations/0006_apikey_policy_profile.down.sql` | Drop column |

## Testing Requirements

- Audit wiring: integration test — make a `tools/call`, query `audit_log`, verify row exists
- DB pool: unit test — verify `SetMaxOpenConns` etc. called with correct values
- CORS: test allowlisted origin gets correct headers, non-allowlisted origin gets no ACAO header, `Vary: Origin` always present, preflight OPTIONS works
- Retention: test with a test DB — insert old rows, run cleanup, verify deleted; verify recent rows preserved
- API key profile: test creating key with profile, verify middleware sets metadata correctly
- Tool call result audit: test successful call logs result, test failed call logs error
- Shutdown: test config parsing, verify server uses configured timeout
- Coverage: >=80% on all modified packages
