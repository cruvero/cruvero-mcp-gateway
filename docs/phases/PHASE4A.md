# Phase 4A: MCP Protocol Handler, Tool Routing, Streaming

## Overview

Implement the MCP protocol layer where the gateway acts as an MCP server. It aggregates capabilities from all registered backends and routes incoming MCP requests to the appropriate backend server.

## Scope

### MCP Server Setup (`internal/proxy/server.go`)
- Initialize mcp-go server with gateway's identity
- Register MCP handlers: tools/list, tools/call, resources/list, resources/read, prompts/list, prompts/get
- Mount MCP endpoint on the main HTTP server (e.g., /mcp or / for root)
- Support Streamable HTTP transport (SSE) as per MCP spec

### Tool Aggregation (`internal/proxy/tools.go`)
- `tools/list` handler:
  - Query capability index for all known tools
  - For each tool, fetch the tool definition (schema) from the owning backend
  - Cache tool definitions with TTL (avoid re-fetching on every list request)
  - Deduplicate by tool name (first registered wins, or configurable priority)
  - Return aggregated tool list to the client
- Tool definition cache with configurable TTL and invalidation on registration changes

### Tool Routing (`internal/proxy/router.go`)
- `tools/call` handler:
  - Look up tool name in capability index -> get list of backend ServerRecords
  - Select backend (round-robin or least-connections among healthy backends)
  - Forward the tool call to the selected backend via HTTP/mTLS
  - Stream response back to client if backend streams
  - Handle backend errors: map to MCP error responses
  - Track request/response for metrics
- Backend selection strategy: configurable (round-robin default)

### Resource Routing (`internal/proxy/resources.go`)
- `resources/list` handler: aggregate resource listings from all backends
- `resources/read` handler: route by resource URI prefix to the owning backend
- Resource URI prefix matching: longest prefix wins

### Backend Client (`internal/proxy/client.go`)
- `BackendClient` struct: manages HTTP connections to a single backend MCP server
- `NewBackendClient(record ServerRecord, tlsConfig *tls.Config) *BackendClient`
- Methods: CallTool, ListTools, ListResources, ReadResource
- Uses mcp-go client for protocol handling
- Configurable timeouts per request
- Connection reuse via http.Client with transport settings

### Streaming (`internal/proxy/stream.go`)
- SSE writer for streaming responses back to MCP clients
- Chunked transfer encoding support
- Heartbeat/keepalive for long-running streams
- Clean stream termination on client disconnect or backend completion

## Files Created

| File | Description |
|------|-------------|
| internal/proxy/server.go | MCP server setup and handler registration |
| internal/proxy/tools.go | Tool aggregation and caching |
| internal/proxy/router.go | Tool call routing and backend selection |
| internal/proxy/resources.go | Resource aggregation and routing |
| internal/proxy/client.go | Backend MCP client |
| internal/proxy/stream.go | SSE streaming support |
| internal/proxy/server_test.go | Server setup tests |
| internal/proxy/tools_test.go | Tool aggregation tests |
| internal/proxy/router_test.go | Routing tests |
| internal/proxy/resources_test.go | Resource routing tests |
| internal/proxy/client_test.go | Backend client tests |

## Testing Requirements

- Tool aggregation: test with multiple backends offering different tools, test deduplication
- Tool routing: test correct backend selection, test round-robin distribution
- Resource routing: test URI prefix matching, test longest prefix wins
- Backend client: test with httptest mock backend, test timeout handling
- Streaming: test SSE format, test client disconnect handling
- Cache: test TTL expiry, test invalidation on registration change
- Coverage: >=80%
