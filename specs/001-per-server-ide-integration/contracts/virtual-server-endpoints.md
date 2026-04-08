# Contract: Virtual Server Endpoints

**Feature**: Per-Server Endpoints & Discovery
**Date**: 2026-04-04

## Endpoints

### 1. Per-Server MCP Passthrough

```
GET|POST /mcp/servers/{serverName}/
```

**Auth**: Required (mTLS | OIDC JWT | API Key)
**Middleware**: Full chain (RequestID → Tracing → Metrics → Auth → RateLimit → ServerScope → Policy → Handler)

**Path Parameters**:
- `serverName`: Backend server name. Case-insensitive, normalized to lowercase. Must match `^[a-z0-9][a-z0-9_-]{0,62}$`.

**Behavior**: Full MCP protocol passthrough to the named backend server. Supports `tools/list`, `tools/call`, `resources/list`, `resources/read`, `prompts/list`, `prompts/get`, and SSE streaming. Tool names returned are bare (no namespace prefix).

**Responses**:

| Status | Condition | Body |
|--------|-----------|------|
| 200 | Success | MCP JSON-RPC response |
| 401 | Unauthenticated | `{"error": "unauthorized"}` |
| 403 | Server scope denied | `{"error": "forbidden", "reason": "server_scope"}` |
| 404 | Server not found | `{"error": "not_found"}` |
| 429 | Rate limited | `{"error": "rate_limited"}` + `Retry-After` header |
| 503 | Server not active | `{"error": "server_unavailable"}` |

**Security**: Unauthenticated requests return 401 without revealing whether the server name exists.

---

### 2. Per-Server SSE Transport

```
GET|POST /mcp/servers/{serverName}/sse
```

Same auth and middleware as above. SSE transport variant for clients that prefer Server-Sent Events.

---

### 3. Auto-Discovery Document

```
GET /.well-known/mcp.json
```

**Auth**: Required
**Cache**: In-memory, TTL configurable via `MCPGW_WELL_KNOWN_CACHE_TTL` (default 5s). Invalidated on server registration/deregistration events.

**Response** (200 OK):

```json
{
  "mcpServers": {
    "todoist": {
      "url": "https://gw.example.com/mcp/servers/todoist/",
      "name": "Todoist MCP Server",
      "version": "1.2.0",
      "status": "active"
    },
    "github": {
      "url": "https://gw.example.com/mcp/servers/github/",
      "name": "GitHub MCP Server",
      "version": "3.0.1",
      "status": "active"
    }
  }
}
```

Only servers with status `active` are included.

**Responses**:

| Status | Condition |
|--------|-----------|
| 200 | Success |
| 401 | Unauthenticated |

---

### 4. Server Listing

```
GET /mcp/servers
```

**Auth**: Required
**Policy**: Respects server scope — viewers see only servers they have access to.

**Response** (200 OK):

```json
[
  {
    "name": "todoist",
    "url": "/mcp/servers/todoist/",
    "version": "1.2.0",
    "status": "active",
    "tool_count": 8,
    "resource_count": 2
  }
]
```

**Responses**:

| Status | Condition |
|--------|-----------|
| 200 | Success (may be empty array) |
| 401 | Unauthenticated |

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MCPGW_PER_SERVER_ENDPOINTS` | `true` | Enable per-server virtual endpoints |
| `MCPGW_WELL_KNOWN_ENABLED` | `true` | Enable `/.well-known/mcp.json` |
| `MCPGW_WELL_KNOWN_CACHE_TTL` | `5s` | Discovery document cache TTL |
