# Data Model: Per-Server Endpoints, Realtime Capability Refresh & IDE Integration

**Branch**: `001-per-server-ide-integration`
**Date**: 2026-04-04

## Entity Changes

### 1. ServerRecord (existing entity — extended)

**Table**: `mcp_servers`

**New fields**:

| Field | Type | Default | Nullable | Description |
|-------|------|---------|----------|-------------|
| `capabilities_hash` | TEXT | `''` | NOT NULL | SHA-256 hash of serialized tool/resource catalog. Used for heartbeat change detection. |
| `routing_strategy` | TEXT | `'round_robin'` | NOT NULL | Routing strategy for this server: `round_robin` or `session_affinity`. |

**Go struct additions** (`internal/types/types.go` — ServerRecord):
- `RoutingStrategy string` — mirrors `routing_strategy` column.

**Note**: `CapabilityHash string` already exists on ServerRecord. Verify
the column exists in the database; migration 0015 adds it if missing.

**State transitions**: No changes to the existing state machine
(pending → approved → active ⇄ stale → expired).

---

### 2. APIKey (existing entity — extended)

**Table**: `api_keys`

**New fields**:

| Field | Type | Default | Nullable | Description |
|-------|------|---------|----------|-------------|
| `server_scope` | TEXT[] | `'{}'` | NOT NULL | List of allowed server names. Empty = gateway-wide access. |

**Go struct additions** (`internal/types/types.go` or store types):
- `ServerScope []string` — populated from `TEXT[]` column.

**Validation rules**:
- Each entry in `server_scope` must match `^[a-z0-9][a-z0-9_-]{0,62}$`.
- Empty array = no restriction (backward compatible).
- Entries are case-insensitive (normalized to lowercase on write).

---

### 3. EmbeddingCache (new in-memory entity)

**No database table** — purely in-memory with LRU eviction.

| Field | Type | Description |
|-------|------|-------------|
| `key` | string | SHA-256 hash of `name + "\n" + description` |
| `embedding` | []float32 | Pre-computed vector embedding |

**Capacity**: Configurable via `MCPGW_EMBEDDING_CACHE_MAX_SIZE` (default: 10000).
**Eviction**: LRU. Oldest-accessed entry evicted when capacity exceeded.
**Lifecycle**: Populated during search index builds. Survives tool
catalog refreshes. Lost on gateway restart (acceptable — rebuilt on
first indexing pass).

---

### 4. BM25Partition (new in-memory entity)

**No database table** — in-memory search index partition.

| Field | Type | Description |
|-------|------|-------------|
| `serverID` | string | Partition key — maps to `mcp_servers.id` |
| `documents` | []Document | Tool documents belonging to this server |
| `nameIdx` | fieldIndex | Inverted index for tool names |
| `titleIdx` | fieldIndex | Inverted index for tool titles |
| `descIdx` | fieldIndex | Inverted index for descriptions |
| `tagsIdx` | fieldIndex | Inverted index for tags |

**Lifecycle**: Created when a server's tools are first indexed. Updated
on capability refresh. Removed when server deregisters or expires.

---

### 5. VirtualServerInstance (new in-memory entity)

**No database table** — cached MCP server instance per backend.

| Field | Type | Description |
|-------|------|-------------|
| `serverName` | string | Normalized backend server name |
| `mcpServer` | *server.MCPServer | mcp-go server instance for this backend |
| `handler` | http.Handler | StreamableHTTP handler |
| `lastRefresh` | time.Time | When tools were last synced |

**Lifecycle**: Lazily created on first request to a per-server endpoint.
Invalidated on capability refresh or server deregistration. Stored in
`sync.Map` on VirtualServerHandler.

---

## Database Migrations

### Migration 0015: server_capabilities_hash

```sql
-- 0015_server_capabilities_hash.up.sql
-- Add capabilities_hash column if not already present.
-- The field may already exist in ServerRecord Go struct but not in DB.
ALTER TABLE mcp_servers
    ADD COLUMN IF NOT EXISTS capabilities_hash TEXT NOT NULL DEFAULT '';

-- 0015_server_capabilities_hash.down.sql
ALTER TABLE mcp_servers DROP COLUMN IF EXISTS capabilities_hash;
```

### Migration 0016: apikey_server_scope

```sql
-- 0016_apikey_server_scope.up.sql
ALTER TABLE api_keys
    ADD COLUMN server_scope TEXT[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN api_keys.server_scope IS
    'Empty = gateway-wide access. Non-empty = restricted to listed server names.';

-- 0016_apikey_server_scope.down.sql
ALTER TABLE api_keys DROP COLUMN server_scope;
```

### Migration 0017: server_routing_strategy

```sql
-- 0017_server_routing_strategy.up.sql
ALTER TABLE mcp_servers
    ADD COLUMN routing_strategy TEXT NOT NULL DEFAULT 'round_robin';

-- 0017_server_routing_strategy.down.sql
ALTER TABLE mcp_servers DROP COLUMN routing_strategy;
```

## Entity Relationship Summary

```
mcp_servers (1) ──── capabilities_hash (change detection)
     |              routing_strategy (round_robin | session_affinity)
     |
     +──── (N) BM25Partition (in-memory, keyed by server ID)
     +──── (1) VirtualServerInstance (in-memory, keyed by server name)
     +──── (N) EmbeddingCache entries (in-memory, keyed by content hash)

api_keys (1) ──── server_scope[] ──── (0..N) mcp_servers.name
```

## New Event Type

| Event | Subject Pattern | Payload |
|-------|----------------|---------|
| `server.capabilities_changed` | `mcpgw.{gateway_id}.events.server.capabilities_changed` | `{ server_id, capabilities_hash, tool_names[] }` |

Added to `internal/events/types.go` alongside existing event types.

## Configuration Variables (new)

| Variable | Type | Default | Entity/Feature |
|----------|------|---------|----------------|
| `MCPGW_PER_SERVER_ENDPOINTS` | bool | `true` | VirtualServerHandler |
| `MCPGW_WELL_KNOWN_ENABLED` | bool | `true` | Discovery document |
| `MCPGW_WELL_KNOWN_CACHE_TTL` | duration | `5s` | Discovery cache |
| `MCPGW_CAPABILITY_REFRESH_ENABLED` | bool | `true` | Heartbeat hash |
| `MCPGW_CAPABILITY_PUSH_ENABLED` | bool | `true` | Push endpoint |
| `MCPGW_EMBEDDING_CACHE_MAX_SIZE` | int | `10000` | EmbeddingCache |
| `MCPGW_SERVER_SCOPE_ENFORCEMENT` | bool | `true` | ServerScope |
| `MCPGW_DEFAULT_ROUTING_STRATEGY` | string | `round_robin` | Router |
| `MCPGW_TOOL_NAMESPACE_MODE` | string | `reject` | NamespaceResolver |
| `MCPGW_NAMESPACE_SEPARATOR` | string | `.` | NamespaceResolver |
