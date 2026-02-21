# Phase 1B: Postgres Store & Migrations

## Overview

Implement the Postgres persistence layer with store interfaces, PostgresServerStore and PostgresAPIKeyStore implementations, and database migrations.

## Scope

### Store Interfaces (`internal/store/`)

**ServerStore**
```go
type ServerStore interface {
    Create(ctx context.Context, record *types.ServerRecord) error
    Get(ctx context.Context, id string) (*types.ServerRecord, error)
    GetByName(ctx context.Context, name string) (*types.ServerRecord, error)
    GetBySPIFFEID(ctx context.Context, spiffeID string) (*types.ServerRecord, error)
    List(ctx context.Context, filter ServerFilter) ([]types.ServerRecord, error)
    Update(ctx context.Context, record *types.ServerRecord) error
    UpdateStatus(ctx context.Context, id string, status types.ServerStatus) error
    UpdateHeartbeat(ctx context.Context, id string) error
    Delete(ctx context.Context, id string) error
    ListStale(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error)
    ListExpired(ctx context.Context, threshold time.Duration) ([]types.ServerRecord, error)
}
```

**APIKeyStore**
```go
type APIKeyStore interface {
    Create(ctx context.Context, key *types.APIKey) error
    GetByLookupHash(ctx context.Context, lookupHash string) (*types.APIKey, error)
    List(ctx context.Context) ([]types.APIKey, error)
    Revoke(ctx context.Context, id string) error
    DeleteExpired(ctx context.Context) (int64, error)
}
```

**AuditStore**
```go
type AuditStore interface {
    Log(ctx context.Context, entry *types.AuditEntry) error
    Query(ctx context.Context, filter AuditFilter) ([]types.AuditEntry, error)
}
```

### Additional Types (`internal/types/types.go` -- append)
- `APIKey` — id, key_lookup_hash, key_bcrypt_hash, name, scopes []string, client_id, expires_at, created_at
- `AuditEntry` — id, event_type, client_id, server_name, details map[string]any, created_at
- `ServerFilter` — optional status, name pattern, limit, offset
- `AuditFilter` — optional event_type, client_id, server_name, since, until, limit

### Migrations
- `migrations/0001_mcp_servers.up.sql` / `.down.sql`
  - Table: mcp_servers (id uuid PK default gen_random_uuid(), name text not null, spiffe_id text not null unique, version text not null default '', host text not null, port integer not null, capabilities jsonb default '{}', status text not null default 'pending', policy_profile text default 'default', last_heartbeat timestamptz, created_at timestamptz default now(), updated_at timestamptz default now())
  - Indexes: idx_mcp_servers_status, idx_mcp_servers_spiffe_id

- `migrations/0002_api_keys.up.sql` / `.down.sql`
  - Table: api_keys (id uuid PK default gen_random_uuid(), key_lookup_hash text unique not null, key_bcrypt_hash text not null, name text not null, scopes text[] not null default '{}', client_id text not null, expires_at timestamptz, created_at timestamptz default now())
  - Indexes: idx_api_keys_lookup_hash, idx_api_keys_client_id
  - Scope values (`read`, `write`, `admin`) are validated in application logic

- `migrations/0003_audit_log.up.sql` / `.down.sql`
  - Table: audit_log (id uuid PK default gen_random_uuid(), event_type text not null, client_id text not null default '', server_name text not null default '', details jsonb default '{}', created_at timestamptz default now())
  - Indexes: idx_audit_log_event_type, idx_audit_log_created_at

### PostgresServerStore Implementation
- Uses database/sql with lib/pq
- Parameterized queries (no string interpolation)
- JSON marshaling for capabilities field
- Proper error wrapping

### PostgresAPIKeyStore Implementation
- Deterministic lookup by key_lookup_hash
- Return key_bcrypt_hash for auth-layer verification
- Scope validation remains in auth/policy layers

### PostgresAuditStore Implementation
- Append-only (no updates or deletes)
- JSON marshaling for details field

## Files Created

| File | Description |
|------|-------------|
| internal/types/types.go | Additional types (APIKey, AuditEntry, filters) |
| internal/store/interfaces.go | Store interfaces |
| internal/store/server_store.go | PostgresServerStore |
| internal/store/apikey_store.go | PostgresAPIKeyStore |
| internal/store/audit_store.go | PostgresAuditStore |
| internal/store/server_store_test.go | Server store tests |
| internal/store/apikey_store_test.go | API key store tests |
| internal/store/audit_store_test.go | Audit store tests |
| migrations/0001_mcp_servers.up.sql | MCP servers table |
| migrations/0001_mcp_servers.down.sql | Drop MCP servers table |
| migrations/0002_api_keys.up.sql | API keys table |
| migrations/0002_api_keys.down.sql | Drop API keys table |
| migrations/0003_audit_log.up.sql | Audit log table |
| migrations/0003_audit_log.down.sql | Drop audit log table |

## Testing Requirements

- Use sqlmock for unit tests (no real database required)
- Test all CRUD operations per store
- Test filter/query operations with various parameters
- Test error cases (not found, duplicate, constraint violations)
- Test JSON marshaling/unmarshaling for jsonb fields
- Coverage: >=80% per package
