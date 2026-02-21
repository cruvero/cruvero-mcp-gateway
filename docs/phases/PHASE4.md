# Phase 4: MCP Proxy & Routing

## Goal

Make the gateway function as a standard MCP server that aggregates capabilities from all registered backend MCP servers and routes tool calls, resource reads, and prompt requests to the correct backend.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [4A](PHASE4A.md) | MCP Protocol Handler, Tool Routing, Streaming | [4 prompts](PHASE4A-PROMPT.md) | proxy |
| [4B](PHASE4B.md) | Circuit Breaker, Retry, Connection Pooling | [3 prompts](PHASE4B-PROMPT.md) | resilience |

## Dependencies

- Phase 1 (Core Foundation) -- types, config, server
- Phase 3 (Registration Protocol) -- capability index for routing decisions

## Deliverables

- Gateway implements MCP server protocol via mcp-go
- tools/list aggregates tools from all registered backends (deduplicated)
- tools/call routes to correct backend based on capability index
- resources/list and resources/read route by resource URI prefix
- Streamable HTTP transport (SSE for server-to-client streaming)
- Request/response correlation (JSON-RPC id tracking)
- Per-backend circuit breaker (closed -> open -> half-open)
- Retry with exponential backoff + jitter
- HTTP connection pool per backend
- Timeout enforcement per request

## Packages Created

- `internal/proxy` -- MCP protocol handler, tool/resource routing, backend communication
- `internal/resilience` -- Circuit breaker, retry logic, connection pool management

## Success Criteria

- Gateway responds to MCP tools/list with aggregated tools from all backends
- tools/call correctly routes to the right backend and returns response
- SSE streaming works for long-running tool calls
- Circuit breaker opens after threshold failures, recovers on half-open success
- Retry handles transient failures with backoff
- Connection pools are managed per backend
- >=80% test coverage
