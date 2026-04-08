# Contract: Server-Scoped API Keys, Session Affinity & Namespacing

**Feature**: Authorization, Routing & Namespace Extensions
**Date**: 2026-04-04

## Server-Scoped API Keys

### API Key Create (existing endpoint — extended)

```
POST /v1/apikeys
```

**Request body** (extended):

```json
{
  "name": "frontend-team",
  "scopes": "tools:call,tools:list",
  "client_id": "frontend-team",
  "policy_profile": "default",
  "server_scope": ["todoist", "github"]
}
```

**New field**: `server_scope` (optional, string array). Empty or omitted =
gateway-wide access (backward compatible).

### CLI Extension

```bash
mcpgw apikey create \
  --name "frontend-team" \
  --scopes "tools:call,tools:list" \
  --server-scope "todoist,github"
```

New flag: `--server-scope` (comma-separated server names).

### Enforcement Behavior

**Per-server endpoint** (`/mcp/servers/{name}/`):
- `ServerScopeMiddleware` extracts `{serverName}` from URL.
- Checks against authenticated identity's `ServerScope`.
- If not in scope → 403 with `X-Denied-Reason: server_scope`.

**Unified endpoint** (`/mcp/`):
- Scope check deferred until after tool-to-server resolution.
- `router.Route()` resolves tool to server, then checks scope.
- If resolved server not in scope → 403.

**Response on denial**:

```json
{
  "error": "forbidden",
  "reason": "server_scope",
  "allowed_servers": ["todoist", "github"],
  "requested_server": "slack"
}
```

### Admin Dashboard

API keys management page shows `server_scope` as a tag list column.
Editable via existing HTMX forms.

---

## Session-Affinity Routing

### Configuration

**Per-server** (via `mcp_servers` table):
- `routing_strategy` column: `round_robin` (default) or `session_affinity`.
- Set during registration or via admin dashboard.

**Global default** (via env var):
- `MCPGW_DEFAULT_ROUTING_STRATEGY`: `round_robin` (default).

### Routing Behavior

| Condition | Strategy Used |
|-----------|--------------|
| Server config = `session_affinity` AND `Mcp-Session-Id` header present | Session affinity (consistent hash) |
| Server config = `session_affinity` AND no session header | Round-robin fallback |
| Server config = `round_robin` (default) | Round-robin regardless of session header |

### Session ID Source

The `Mcp-Session-Id` HTTP header, per MCP Streamable HTTP transport spec.
The gateway treats the value as an opaque string — no parsing or validation.

### Registration Extension

```json
{
  "service_name": "assistant",
  "version": "2.0.0",
  "capabilities": { ... },
  "routing_strategy": "session_affinity"
}
```

New optional field in registration request.

---

## Tool Namespace Isolation

### Namespace Modes

| Mode | `MCPGW_TOOL_NAMESPACE_MODE` | Behavior on Unified Endpoint |
|------|-----------------------------|------------------------------|
| Reject (default) | `reject` | Registration fails if tool name conflicts |
| Always namespace | `namespace_always` | All tools prefixed: `{server}.{tool}` |
| Namespace on conflict | `namespace_on_conflict` | Only conflicting names prefixed; unique names stay bare |

### Separator

Configurable via `MCPGW_NAMESPACE_SEPARATOR` (default: `.`).

### tools/list Response (unified endpoint, namespace_always mode)

```json
{
  "tools": [
    {
      "name": "todoist.create_task",
      "description": "Create a new task in Todoist",
      "inputSchema": { ... }
    },
    {
      "name": "github.create_issue",
      "description": "Create a GitHub issue",
      "inputSchema": { ... }
    }
  ]
}
```

### tools/call Resolution (unified endpoint)

1. Namespaced name (e.g., `todoist.create_task`):
   - Split on first separator → server = `todoist`, tool = `create_task`.
   - Look up `create_task` filtered to server `todoist`.
   - Route to resolved backend.

2. Bare name (e.g., `create_task`):
   - Look up in CapabilityIndex.
   - If unique → route directly.
   - If ambiguous → return error with available namespaced alternatives.

### Per-Server Endpoint Behavior

Per-server endpoints (`/mcp/servers/{name}/`) always return bare tool
names — no namespace prefix. Namespace mode has no effect on per-server
endpoints.

---

## Configuration Summary

| Variable | Default | Feature |
|----------|---------|---------|
| `MCPGW_SERVER_SCOPE_ENFORCEMENT` | `true` | Server-scoped keys |
| `MCPGW_DEFAULT_ROUTING_STRATEGY` | `round_robin` | Session affinity |
| `MCPGW_TOOL_NAMESPACE_MODE` | `reject` | Namespace isolation |
| `MCPGW_NAMESPACE_SEPARATOR` | `.` | Namespace separator |
