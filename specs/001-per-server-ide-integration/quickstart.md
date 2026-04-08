# Quickstart: Per-Server Endpoints & IDE Integration

**Branch**: `001-per-server-ide-integration`

## Prerequisites

- MCP Gateway running with at least one registered backend server.
- Valid authentication credential (API key or OIDC token).

## 1. Discover Available Servers

```bash
# Fetch the discovery document
curl -s -H "Authorization: Bearer mcpgw_YOUR_API_KEY" \
  https://gateway.example.com/.well-known/mcp.json | jq .

# Or list servers with metadata
curl -s -H "Authorization: Bearer mcpgw_YOUR_API_KEY" \
  https://gateway.example.com/mcp/servers | jq .
```

## 2. Configure an IDE Client

### VS Code Copilot (mcp.json)

```json
{
  "mcpServers": {
    "todoist": {
      "url": "https://gateway.example.com/mcp/servers/todoist/",
      "headers": {
        "Authorization": "Bearer mcpgw_YOUR_API_KEY"
      }
    }
  }
}
```

### Claude Code (settings)

```json
{
  "mcpServers": {
    "todoist": {
      "command": "npx",
      "args": ["-y", "@anthropic/mcp-remote", "https://gateway.example.com/mcp/servers/todoist/"],
      "env": {
        "API_KEY": "mcpgw_YOUR_API_KEY"
      }
    }
  }
}
```

### Cursor

Add the per-server URL in Cursor's MCP server settings. Use the URL
from the discovery document.

## 3. Verify Connection

Once configured, the IDE client will:
1. Call `tools/list` on the per-server endpoint.
2. Receive only the tools from that specific backend.
3. Allow tool calls routed through the gateway with full auth and policy.

## 4. Create a Server-Scoped API Key

```bash
mcpgw apikey create \
  --name "ide-todoist-only" \
  --scopes "tools:call,tools:list" \
  --server-scope "todoist"
```

This key can only access the todoist backend. Requests to other servers
return 403.

## 5. Enable Capability Refresh (Fleet Servers)

Fleet servers automatically participate in capability refresh by including
a `capabilities_hash` in heartbeat payloads. For immediate updates:

```bash
# Push refresh (fleet servers with mTLS)
curl -X POST --cert client.pem --key client-key.pem \
  https://gateway.example.com/v1/registrations/{id}/capabilities
```

## 6. Session Affinity (Stateful Backends)

Register a stateful backend with session affinity:

```json
{
  "service_name": "assistant",
  "version": "2.0.0",
  "capabilities": { "tools": ["chat", "summarize"] },
  "routing_strategy": "session_affinity"
}
```

Clients sending the `Mcp-Session-Id` header will be consistently routed
to the same replica.

## Validation Checklist

- [ ] `GET /.well-known/mcp.json` returns active servers
- [ ] `GET /mcp/servers` returns server list with tool counts
- [ ] IDE client connects via per-server URL and lists tools
- [ ] Tool calls via per-server endpoint succeed
- [ ] Scoped API key is denied access to out-of-scope servers
- [ ] Heartbeat with changed hash triggers search index update
- [ ] Push refresh endpoint updates search results immediately
