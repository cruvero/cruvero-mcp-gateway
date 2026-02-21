# Phase 1B Implementation Prompts

## Prompt 1 of 3: Migrations & Store Interfaces

### Required Reading (read these files before writing code)
- docs/phases/PHASE1B.md
- internal/types/types.go
- LLM.md

### Interface contract
```go
type APIKeyStore interface {
    Create(ctx context.Context, key *types.APIKey) error
    GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error)
    List(ctx context.Context) ([]types.APIKey, error)
    Revoke(ctx context.Context, id string) error
    DeleteExpired(ctx context.Context) (int64, error)
}
```

### Task

Create database migrations and store interfaces.

1. Create migration files:
   - `migrations/0001_mcp_servers.up.sql`: Create mcp_servers table with columns (id uuid PK default gen_random_uuid(), name text not null, spiffe_id text not null unique, version text not null default '', host text not null, port integer not null, capabilities jsonb default '{}', status text not null default 'pending', policy_profile text default 'default', last_heartbeat timestamptz, created_at timestamptz default now(), updated_at timestamptz default now()). Add indexes on status, spiffe_id.
   - `migrations/0001_mcp_servers.down.sql`: Drop table
   - `migrations/0002_api_keys.up.sql`: Create api_keys table (id uuid PK default gen_random_uuid(), key_lookup_hash text unique not null, key_bcrypt_hash text not null, name text not null, scopes text[] not null default '{}', client_id text not null, expires_at timestamptz, created_at timestamptz default now()). Indexes on key_lookup_hash, client_id.
   - `migrations/0002_api_keys.down.sql`: Drop table
   - `migrations/0003_audit_log.up.sql`: Create audit_log table (id uuid PK default gen_random_uuid(), event_type text not null, client_id text not null default '', server_name text not null default '', details jsonb default '{}', created_at timestamptz default now()). Indexes on event_type, created_at.
   - `migrations/0003_audit_log.down.sql`: Drop table

2. Create `internal/store/interfaces.go` with ServerStore, APIKeyStore, and AuditStore interfaces as specified in PHASE1B.md

3. Add additional types to `internal/types/types.go`: APIKey (with `key_lookup_hash` + `key_bcrypt_hash`), AuditEntry, ServerFilter, AuditFilter

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Migrations are valid SQL
- Interfaces cover all required operations
- Types have JSON tags

---

## Prompt 2 of 3: PostgresServerStore & PostgresAPIKeyStore

### Required Reading (read these files before writing code)
- internal/store/interfaces.go
- internal/types/types.go
- migrations/0001_mcp_servers.up.sql
- migrations/0002_api_keys.up.sql

### Task

Implement the Postgres store for MCP servers and API keys.

1. `internal/store/server_store.go`: PostgresServerStore implementing ServerStore
   - Constructor: NewPostgresServerStore(db *sql.DB) *PostgresServerStore
   - All methods use parameterized queries
   - JSON marshal/unmarshal for capabilities (jsonb)
   - UpdateHeartbeat also sets updated_at to now()
   - ListStale: where last_heartbeat < now() - threshold AND status = 'active'
   - ListExpired: where last_heartbeat < now() - (3 * threshold) AND status IN ('active', 'stale')
   - Proper error wrapping with fmt.Errorf("server store: %w", err)

2. `internal/store/apikey_store.go`: PostgresAPIKeyStore implementing APIKeyStore
   - Constructor: NewPostgresAPIKeyStore(db *sql.DB) *PostgresAPIKeyStore
   - GetByLookupHash: lookup by key_lookup_hash, check expires_at not passed
   - Revoke: soft delete by setting expires_at to now()
   - DeleteExpired: delete where expires_at < now(), return count

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- No SQL injection (parameterized queries only)
- Proper error wrapping
- JSON handling for jsonb columns

---

## Prompt 3 of 3: PostgresAuditStore & Tests

### Required Reading (read these files before writing code)
- internal/store/interfaces.go
- internal/store/server_store.go
- internal/store/apikey_store.go
- internal/types/types.go

### Task

Implement the audit store and write tests for all stores.

1. `internal/store/audit_store.go`: PostgresAuditStore implementing AuditStore
   - Log: insert audit entry with JSON details
   - Query: filter by event_type, client_id, server_name, time range, with limit/offset

2. Tests using sqlmock (github.com/DATA-DOG/go-sqlmock):
   - `internal/store/server_store_test.go`: Test Create, Get, GetByName, List, Update, UpdateStatus, UpdateHeartbeat, Delete, ListStale, ListExpired. Test not-found and duplicate errors.
   - `internal/store/apikey_store_test.go`: Test Create, GetByLookupHash (valid and expired), List, Revoke, DeleteExpired.
   - `internal/store/audit_store_test.go`: Test Log, Query with various filters.

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All store methods tested
- Error cases covered
- >=80% coverage per file
- sqlmock expectations fully verified
