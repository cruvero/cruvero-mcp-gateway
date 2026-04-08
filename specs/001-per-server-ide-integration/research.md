# Research: Per-Server Endpoints, Realtime Capability Refresh & IDE Integration

**Branch**: `001-per-server-ide-integration`
**Date**: 2026-04-04

## 1. RoutingStrategy Interface Extension for Session Affinity

**Decision**: Extend `RoutingStrategy.Select` signature to accept context
and a routing request struct containing session ID.

**Rationale**: The current `Select(candidates []types.ServerRecord)
*types.ServerRecord` signature has no way to pass session ID or server
config. Adding a `RoutingRequest` struct is forward-compatible for future
routing signals (priority, locality, etc.).

**Current signature** (`internal/proxy/router.go`):
```go
type RoutingStrategy interface {
    Select(candidates []types.ServerRecord) *types.ServerRecord
}
```

**Proposed signature**:
```go
type RoutingRequest struct {
    SessionID string
    ToolName  string
}

type RoutingStrategy interface {
    Select(ctx context.Context, candidates []types.ServerRecord, req *RoutingRequest) (*types.ServerRecord, error)
}
```

**Alternatives considered**:
- Passing session ID via context: Too implicit, not type-safe.
- Separate interface for session-aware strategies: Fragmented, harder to
  compose in Router.

**Migration path**: Update `RoundRobinStrategy.Select` to accept new
signature (ignore `req`). Update all call sites in `router.go`.

---

## 2. BM25 Partitioned Index Strategy

**Decision**: Partition the BM25 inverted index by server ID. Each server
gets its own document set and term frequency maps. IDF is computed lazily
across all partitions on first query after a partition change.

**Rationale**: The current `BM25Engine.Index()` replaces the entire index.
Incremental reindex of one server (e.g., 1 tool changed on a 100-tool
server) would require re-indexing all 100+ tools across all servers.
Partitioning makes updates O(tools_in_changed_server) not O(total_tools).

**Current structure** (`internal/search/bm25.go`):
```go
type BM25Engine struct {
    mu       sync.RWMutex
    docs     []Document
    synonyms SynonymProvider
    nameIdx  fieldIndex
    titleIdx fieldIndex
    descIdx  fieldIndex
    tagsIdx  fieldIndex
}
```

**Proposed structure**:
```go
type BM25Engine struct {
    mu         sync.RWMutex
    partitions map[string]*bm25Partition  // key: serverID
    synonyms   SynonymProvider
    globalIDF  map[string]float64
    idfDirty   atomic.Bool
    totalDocs  int
}

type bm25Partition struct {
    serverID string
    docs     []Document
    nameIdx  fieldIndex
    titleIdx fieldIndex
    descIdx  fieldIndex
    tagsIdx  fieldIndex
}
```

**New methods**:
- `AddPartition(serverID string, docs []Document) error`
- `RemovePartition(serverID string)`
- `UpdatePartition(serverID string, docs []Document) error`
- Existing `Index()` and `Search()` preserved for backward compatibility.

**Alternatives considered**:
- Full rebuild on every change: Too slow at scale (100+ tools).
- Separate BM25Engine per server: Breaks cross-server IDF and ranking.

---

## 3. Vector Embedding Cache Design

**Decision**: LRU cache keyed by SHA-256 of `name + "\n" + description`.
Cache stores `[]float32` embeddings. Max size configurable via
`MCPGW_EMBEDDING_CACHE_MAX_SIZE` (default 10000).

**Rationale**: ONNX embedding is the most expensive operation in the search
pipeline (~10-50ms per document). When a server's tools change, most tools
are unchanged. Caching by content hash avoids re-embedding stable tools.

**Proposed implementation** (`internal/search/embedding_cache.go`):
```go
type EmbeddingCache struct {
    mu       sync.RWMutex
    cache    map[string][]float32  // key: SHA-256(name + "\n" + description)
    order    []string              // LRU order
    maxSize  int
}

func (c *EmbeddingCache) Get(key string) ([]float32, bool)
func (c *EmbeddingCache) Set(key string, embedding []float32)
func (c *EmbeddingCache) ContentHash(name, description string) string
```

**VectorEngine changes**: Add server-partitioned storage parallel to BM25.
On incremental update: compute content hashes, check cache, embed only
new/changed tools, evict removed tools.

**Alternatives considered**:
- No cache, always re-embed: Violates SC-010 (incremental reindex).
- File-based cache: Adds filesystem dependency, slower than in-memory.
- Redis/Dragonfly cache: Over-engineered for single-pod common case.

---

## 4. VirtualServerHandler Design

**Decision**: New handler in `internal/proxy/virtual_server.go` that
resolves `{serverName}` from the URL path, looks up the server in
CapabilityIndex, validates status, and forwards the full MCP request
to the backend via an ad-hoc `mcp-go` StreamableHTTPServer per server.

**Rationale**: Per-server endpoints must be fully MCP-compliant (tools/list,
tools/call, resources, prompts, SSE). Reusing the existing ProxyServer's
StreamableHTTPServer is not feasible because it aggregates tools from all
backends. Each virtual endpoint needs its own MCP server instance that
exposes only the target backend's tools.

**Request flow**:
1. Extract `{serverName}` from chi URL param.
2. Validate against `^[a-z0-9][a-z0-9_-]{0,62}$`.
3. Lookup in CapabilityIndex by name (case-insensitive, normalized).
4. If not found or not active → 404 / 503.
5. Get or create a per-server MCP server instance (cached, lazy).
6. Forward request to that instance's StreamableHTTP handler.

**Caching strategy**: Per-server MCP server instances are cached in a
`sync.Map` keyed by server name. Invalidated when the server deregisters
or its tool catalog changes (capabilities refresh event).

**Alternatives considered**:
- Simple HTTP reverse proxy (no MCP server): Loses MCP session management,
  SSE streaming, and protocol validation.
- Reuse ProxyServer with server filter: Too invasive to existing code,
  breaks aggregation behavior.

---

## 5. Discovery Document Caching

**Decision**: In-memory cache with configurable TTL (default 5s).
Invalidated on `server.registered` and `server.deregistered` events via
the existing Broadcaster.

**Rationale**: The discovery document is generated by iterating all active
servers in the CapabilityIndex. Under load this should not hit the database.
Short TTL ensures freshness; event-driven invalidation handles registration
changes instantly.

**Implementation**: `atomic.Pointer[cachedDiscoveryDoc]` with timestamp.
Thread-safe, lock-free reads. Regeneration under mutex on cache miss.

---

## 6. ServerScope Middleware Placement

**Decision**: Insert `ServerScopeMiddleware` after auth middleware and
before the proxy handler in the middleware chain.

**Rationale**: Scope check requires an authenticated identity (to read
`ServerScope` from the API key). On per-server endpoints, the target
server is known from the URL. On the unified endpoint, scope is checked
after tool-to-server resolution inside the proxy handler.

**Middleware chain order** (updated):
```
RequestID → Tracing → Metrics → Logging → Recovery → BodyLimit → CORS
→ Auth → RateLimit → ServerScope → Policy → Handler
```

**Two enforcement points**:
1. Per-server endpoint: ServerScopeMiddleware checks `{serverName}`
   against key's `ServerScope` before reaching the handler.
2. Unified endpoint: Proxy handler checks scope after
   `CapabilityIndex.FindServers(toolName)` resolves the target server.

---

## 7. Namespace Resolver Integration Points

**Decision**: NamespaceResolver modifies tool names during `tools/list`
aggregation (in `tools.go:handleListTools`) and resolves them during
`tools/call` routing (in `router.go:resolveFederatedName`).

**Rationale**: The existing `federatedToolName()` function already
prefixes tool names with server display names. The namespace resolver
extends this with conflict-aware modes. Integration into existing
federated naming is natural.

**Three modes**:
- `reject` (default): Current behavior. Registration fails on conflict.
- `namespace_always`: All tools prefixed with `{serverName}.{toolName}`.
- `namespace_on_conflict`: Only conflicting names get prefixed; unique
  names stay bare.

**Conflict tracking**: CapabilityIndex already maintains
`tools: map[string][]ServerRecord`. A tool name with `len(servers) > 1`
is a conflict. This is sufficient for conflict detection without new state.

---

## 8. Rendezvous Hashing for Session Affinity

**Decision**: Use rendezvous (HRW) hashing. For each candidate, compute
`hash(sessionID + candidateID)`. Highest hash wins.

**Rationale**: Rendezvous hashing is stateless (no routing tables),
handles replica additions/removals gracefully (only affected sessions
redistribute), and is O(n) in candidates (typically 2-5 replicas).

**Hash function**: FNV-1a 64-bit. Fast, well-distributed, no
cryptographic overhead needed.

**Alternatives considered**:
- Consistent hash ring (ketama): More complex, needs virtual nodes,
  unnecessary for small candidate sets.
- Sticky session table: Requires shared state across pods, adds a
  failure mode.
- Client-provided routing hint: Not part of MCP protocol.

---

## 9. Existing CapabilityHash Infrastructure

**Finding**: `HeartbeatRequest` already has a `CapabilityHash` field
(`internal/registration/heartbeat.go:29`). `ServerRecord` already has
a `CapabilityHash` field (`internal/types/types.go:91`). However, the
`Heartbeat()` method in `service.go` does not compare the hash or
trigger a refresh on mismatch.

**Decision**: Extend the existing `Heartbeat()` method to compare
`req.CapabilityHash` with `server.CapabilityHash`. On mismatch, call a
new `RefreshCapabilities()` function that fetches `tools/list` from the
backend, updates the CapabilityIndex and search indexes, stores the new
hash, and broadcasts a change event.

**Migration 0015 note**: The `capabilities_hash` column may already exist
in the database if it was added during an earlier migration. Verify before
creating migration 0015. If the column exists, migration 0015 becomes a
no-op or is skipped.

---

## 10. mcp-go v0.45.0 Tool Fields

**Finding**: The `mcp.Tool` struct in mcp-go v0.45.0 has expanded fields
beyond what `ToolDefinition` captures. Current `ToolDefinition`
(`internal/proxy/types.go`) includes Name, Description, InputSchema, and
several additional fields (Annotations, Icons, Execution, Meta,
DeferLoading). The `BackendClient.ListTools()` method in `client.go`
already preserves these fields.

**Decision**: No changes needed to `ToolDefinition` for this feature.
The existing type already captures all fields needed for per-server
passthrough and namespace resolution.
