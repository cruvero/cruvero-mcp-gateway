# Phase 4A Implementation Prompts

## Prompt 1 of 4: Backend Client & MCP Server Setup

### Required Reading (read these files before writing code)
- docs/phases/PHASE4A.md
- internal/types/types.go
- internal/registration/index.go
- internal/identity/tls.go
- internal/config/config.go

### Dependency note
- This prompt assumes Phase 3B has been completed and `internal/registration/index.go` with `CapabilityIndex` already exists.

### Task

Create the backend client and MCP server foundation.

1. `internal/proxy/client.go`:
   - Define BackendClient struct: record types.ServerRecord, httpClient *http.Client, logger *slog.Logger
   - NewBackendClient(record types.ServerRecord, tlsConfig *tls.Config, timeout time.Duration) *BackendClient
   - Configure http.Client with transport: MaxIdleConnsPerHost, IdleConnTimeout, TLSClientConfig
   - CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error): POST to backend's MCP endpoint
   - ListTools(ctx context.Context) ([]ToolDefinition, error): call tools/list on backend
   - ListResources(ctx context.Context) ([]ResourceDefinition, error): call resources/list on backend
   - ReadResource(ctx context.Context, uri string) (*ResourceContent, error): call resources/read
   - Define ToolResult, ToolDefinition, ResourceDefinition, ResourceContent types in proxy/types.go

2. `internal/proxy/types.go`:
   - ToolDefinition: Name, Description, InputSchema (json.RawMessage)
   - ToolResult: Content []ContentBlock, IsError bool
   - ContentBlock: Type, Text (aligned with MCP spec)
   - ResourceDefinition: URI, Name, Description, MimeType
   - ResourceContent: URI, MimeType, Text

3. `internal/proxy/server.go`:
   - Define ProxyServer struct: index *registration.CapabilityIndex, clients map[string]*BackendClient, config, logger
   - NewProxyServer constructor
   - SetupMCP() method: initialize mcp-go server, register handlers
   - Handler() http.Handler: return the mcp-go HTTP handler for mounting in chi

4. Tests for BackendClient using httptest mock servers

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Backend client correctly communicates with MCP servers
- Types align with MCP protocol specification
- ProxyServer initializes correctly

---

## Prompt 2 of 4: Tool Aggregation & Caching

### Required Reading (read these files before writing code)
- internal/proxy/server.go
- internal/proxy/client.go
- internal/proxy/types.go
- internal/registration/index.go

### Task

Implement tool aggregation with caching.

1. `internal/proxy/tools.go`:
   - Define ToolCache struct: mu sync.RWMutex, tools map[string]cachedTool, ttl time.Duration
   - cachedTool: definition ToolDefinition, fetchedAt time.Time, serverID string
   - NewToolCache(ttl time.Duration) *ToolCache
   - Get(name string) (ToolDefinition, bool): return if not expired
   - Set(name string, def ToolDefinition, serverID string): store with timestamp
   - Invalidate(): clear all entries (called on registration changes)
   - InvalidateServer(serverID string): clear entries from a specific server
   - handleListTools(ctx context.Context) ([]ToolDefinition, error):
     - Get all tool names from capability index
     - For each, check cache; if miss, fetch from backend client
     - Deduplicate by name (first registered server wins)
     - Return aggregated list

2. `internal/proxy/tools_test.go`:
   - Test cache hit returns cached definition
   - Test cache miss triggers backend fetch
   - Test cache expiry triggers re-fetch
   - Test Invalidate clears all entries
   - Test deduplication when multiple backends offer same tool
   - Test aggregation with multiple backends

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Tool list is correctly aggregated from all backends
- Cache reduces backend calls
- Deduplication works correctly

---

## Prompt 3 of 4: Tool & Resource Routing

### Required Reading (read these files before writing code)
- internal/proxy/server.go
- internal/proxy/tools.go
- internal/proxy/client.go
- internal/registration/index.go

### Task

Implement routing for tool calls and resource requests.

1. `internal/proxy/router.go`:
   - Define Router struct: index *CapabilityIndex, clients sync.Map (serverID -> *BackendClient), strategy RoutingStrategy, logger
   - RoutingStrategy interface: Select(candidates []types.ServerRecord) *types.ServerRecord
   - RoundRobinStrategy: atomic counter mod len(candidates)
   - Route(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error):
     - Lookup tool in index
     - If no backends found, return MCP error "tool not found"
     - Select backend via strategy
     - Call backend via BackendClient.CallTool
     - Return result
   - GetOrCreateClient(record types.ServerRecord) *BackendClient: lazy client creation

2. `internal/proxy/resources.go`:
   - handleListResources: aggregate from all backends
   - handleReadResource(uri string): find backend by longest matching URI prefix in index, forward request
   - Error if no backend matches the resource URI

3. Tests:
   - `internal/proxy/router_test.go`: Test routing to correct backend, test round-robin distribution, test tool not found error
   - `internal/proxy/resources_test.go`: Test resource listing aggregation, test URI prefix routing, test no match error

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Tool calls route to the correct backend
- Round-robin distributes across healthy backends
- Resource routing uses longest prefix match
- Clear error responses for unknown tools/resources

---

## Prompt 4 of 4: SSE Streaming & Integration

### Required Reading (read these files before writing code)
- internal/proxy/server.go
- internal/proxy/router.go
- internal/server/server.go

### Task

Implement SSE streaming and wire the proxy into the main server.

1. `internal/proxy/stream.go`:
   - SSEWriter struct wrapping http.ResponseWriter with flusher
   - WriteEvent(event, data string) error: write SSE formatted event
   - WriteToolResult(result *ToolResult) error: serialize and send as SSE
   - Keepalive(ctx context.Context, interval time.Duration): send comment lines as heartbeat
   - Detect client disconnect via context cancellation

2. Wire proxy into main server:
   - Mount ProxyServer.Handler() at the MCP endpoint path
   - Ensure middleware chain applies (auth, rate limiting to be added later)
   - The MCP endpoint should be accessible to authenticated clients

3. Integration-style test:
   - Create mock backend MCP servers (httptest)
   - Register them in capability index
   - Send tools/list via the proxy -> verify aggregated response
   - Send tools/call -> verify routing and response
   - Test SSE streaming format

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- SSE streaming works with proper format
- Proxy is mounted in the main server
- End-to-end tool list and call work through the proxy
- >=80% coverage across proxy package
