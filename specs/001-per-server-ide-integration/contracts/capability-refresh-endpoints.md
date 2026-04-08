# Contract: Capability Refresh Endpoints

**Feature**: Realtime Capability Refresh
**Date**: 2026-04-04

## Endpoints

### 1. Push Capability Refresh

```
POST /v1/registrations/{id}/capabilities
```

**Auth**: mTLS required. Caller's SPIFFE ID must match the registered server's `spiffe_id`.
**Rate Limit**: 10 requests/minute per server.

**Behavior**: Triggers an immediate tool catalog refresh for the specified server. The gateway fetches `tools/list` and `resources/list` from the backend, updates the CapabilityIndex and search indexes, stores the new capabilities hash, broadcasts a change event to other pods, and invalidates ToolCache entries for this server.

**Response** (200 OK):

```json
{
  "tools_count": 12,
  "resources_count": 3,
  "reindex_triggered": true,
  "capabilities_hash": "d4e5f6a7b8c9..."
}
```

**Responses**:

| Status | Condition | Body |
|--------|-----------|------|
| 200 | Refresh completed | Counts + new hash |
| 401 | No mTLS identity | `{"error": "unauthorized"}` |
| 403 | SPIFFE ID mismatch | `{"error": "forbidden", "reason": "identity_mismatch"}` |
| 404 | Registration not found | `{"error": "not_found"}` |
| 429 | Rate limited | `{"error": "rate_limited"}` + `Retry-After` header |

---

### 2. Heartbeat with Capabilities Hash (existing endpoint — extended)

```
POST /v1/registrations/{id}/heartbeat
```

**Request body** (extended):

```json
{
  "status": "ok",
  "registration_id": "...",
  "lease_epoch": 5,
  "capabilities_hash": "a1b2c3d4..."
}
```

**New field**: `capabilities_hash` (optional). If provided and differs from
the stored hash, the gateway triggers a capability refresh inline before
responding.

**Response body** (extended):

```json
{
  "server_status": "active",
  "next_deadline": "2026-04-04T12:00:30Z",
  "lease_epoch": 5,
  "capabilities_hash": "a1b2c3d4..."
}
```

**New field in response**: `capabilities_hash` — echoes the current stored
hash (may be updated if refresh was triggered).

**Behavior on hash mismatch**:
1. Call `tools/list` on the backend.
2. Update CapabilityIndex.
3. Trigger incremental search reindex.
4. Store new hash.
5. Broadcast `server.capabilities_changed` event.
6. Invalidate ToolCache entries for this server.

**Fallback**: If the backend is unreachable during refresh, retain existing
catalog and retry on next heartbeat. Do not fail the heartbeat itself.

---

## Events

### server.capabilities_changed

**Subject**: `mcpgw.{gateway_id}.events.server.capabilities_changed`

**Payload**:

```json
{
  "server_id": "uuid-...",
  "capabilities_hash": "d4e5f6a7...",
  "tool_names": ["create_task", "list_tasks", "delete_task"]
}
```

**Subscribers**: Other gateway pods. On receipt, each pod calls `tools/list`
on the affected backend, updates its local CapabilityIndex and search
indexes.

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MCPGW_CAPABILITY_REFRESH_ENABLED` | `true` | Enable heartbeat hash comparison |
| `MCPGW_CAPABILITY_PUSH_ENABLED` | `true` | Enable push refresh endpoint |
| `MCPGW_EMBEDDING_CACHE_MAX_SIZE` | `10000` | Max cached embeddings (LRU) |
