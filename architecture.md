# MCP Gateway Architecture

This document describes the architecture of the MCP Gateway as derived from the source code. It covers the system's design, components, data flow, and deployment model.

---

## Table of Contents

1. [Overview](#overview)
2. [High-Level Architecture](#high-level-architecture)
3. [Startup & Initialization](#startup--initialization)
4. [Request Lifecycle](#request-lifecycle)
5. [Authentication & Authorization](#authentication--authorization)
6. [Backend Server Management](#backend-server-management)
7. [Proxy & Routing](#proxy--routing)
8. [Per-Server Virtual Endpoints](#per-server-virtual-endpoints)
9. [Resilience Layer](#resilience-layer)
10. [Rate Limiting](#rate-limiting)
11. [Policy Engine & Tool Classification](#policy-engine--tool-classification)
12. [Progressive Discovery & Search](#progressive-discovery--search)
13. [Realtime Capability Refresh](#realtime-capability-refresh)
14. [Orchestrator](#orchestrator)
15. [Events & Cross-Pod Synchronization](#events--cross-pod-synchronization)
16. [Admin Dashboard](#admin-dashboard)
17. [Observability](#observability)
18. [Database Schema](#database-schema)
19. [Kubernetes Deployment](#kubernetes-deployment)
20. [CLI Subcommands](#cli-subcommands)
21. [Package Map](#package-map)

---

## Overview

The MCP Gateway is a production-grade reverse proxy and aggregator for [Model Context Protocol](https://modelcontextprotocol.io) (MCP) servers, written in Go. It sits between AI clients (IDEs, LLM agents) and one or more MCP backend servers, providing a single entry point with unified authentication, rate limiting, policy enforcement, tool discovery, and observability.

The gateway exposes two endpoint modes: a **unified `/mcp/` endpoint** that aggregates tools from all backends (for LLM agents and the Cruvero platform), and **per-server virtual endpoints** at `/mcp/servers/{name}/` that provide individual MCP connections for IDE clients (Copilot, Cursor, Claude Code, Windsurf). An auto-discovery document at `/.well-known/mcp.json` lists all available per-server URLs.

```
 IDE Clients (Copilot, Cursor, Claude Code)    LLM Agents / Cruvero
       |              |              |                  |
       v              v              v                  v
  /mcp/servers/   /mcp/servers/   /mcp/servers/     /mcp/
   todoist/        github/         slack/       (unified, aggregated)
       |              |              |                  |
       +------+-------+------+------+      +-----------+
              |              |             |
              v              v             v
  +-----------------------------------------------------------+
  |                    MCP Gateway (:8443)                     |
  |                                                           |
  |  /.well-known/mcp.json   (auto-discovery document)       |
  |  GET /mcp/servers         (server listing with metadata)  |
  |                                                           |
  |  Middleware Chain:                                         |
  |    RequestID -> Tracing -> Metrics -> Auth -> ServerScope  |
  |    -> RateLimit -> Policy -> Handler                       |
  |                                                           |
  |  Routing: RoundRobin | SessionAffinity (per-server)       |
  |  Namespacing: reject | namespace_always | on_conflict     |
  +------+----------+-----------+                             |
         |          |           |                              |
    +----+----+ +---+-----+ +--+------+                       |
    | Backend | | Backend  | | Backend |  ...                  |
    | MCP Srv | | MCP Srv  | | MCP Srv |                      |
    +---------+ +----------+ +---------+                      |
  +-----------------------------------------------------------+
```

---

## High-Level Architecture

```
+------------------------------------------------------------------+
|                        MCP Gateway                               |
|                                                                  |
|  +------------------+  +------------------+  +----------------+  |
|  | HTTP Server      |  | Metrics Server   |  | Admin UI       |  |
|  | :8443 (TLS)      |  | :9090 (Prometheus|  | /admin/*       |  |
|  +--------+---------+  +------------------+  +----------------+  |
|           |                                                      |
|  +--------v---------------------------------------------------+  |
|  |              Middleware Chain                               |  |
|  |  RequestID -> Tracing -> Metrics -> Logging -> CORS        |  |
|  |  -> Auth (mTLS | OIDC | API Key) -> Server Scope           |  |
|  |  -> Rate Limit -> Policy Enforcement                       |  |
|  +--------+---------------------------------------------------+  |
|           |                                                      |
|  +--------v---------------------------------------------------+  |
|  |              Core Services                                 |  |
|  |                                                            |  |
|  |  +---------------+  +--------------+  +----------------+  |  |
|  |  | Proxy Server  |  | Registration |  | Event          |  |  |
|  |  | (tool routing,|  | Service      |  | Publisher      |  |  |
|  |  |  caching,     |  | (lifecycle,  |  | (NATS/         |  |  |
|  |  |  discovery)   |  |  heartbeats, |  |  Dragonfly)    |  |  |
|  |  +-------+-------+  |  sweeper)    |  +----------------+  |  |
|  |          |           +--------------+                      |  |
|  |  +-------v-------+  +----------------+                     |  |
|  |  | Router        |  | Virtual Server |                     |  |
|  |  | (round-robin, |  | Handler        |                     |  |
|  |  |  session      |  | (per-server    |                     |  |
|  |  |  affinity,    |  |  MCP proxy)    |                     |  |
|  |  |  circuit      |  +----------------+                     |  |
|  |  |  breaker,     |                                        |  |
|  |  |  retry,       |  +----------------+  +--------------+  |  |
|  |  |  namespace)   |  | Orchestrator   |  | Search       |  |  |
|  |  +-------+-------+  | (LLM-driven    |  | (BM25,       |  |  |
|  |          |           |  multi-step    |  |  vector,     |  |  |
|  |          |           |  planning)     |  |  hybrid,     |  |  |
|  |          |           +----------------+  |  partitioned)|  |  |
|  |          |                               +--------------+  |  |
|  |          |                                                 |  |
|  +------------------------------------------------------------------------+
|           |                                                      |
|  +--------v---------------------------------------------------+  |
|  |              Data Layer                                    |  |
|  |  PostgreSQL (servers, api_keys, audit_log, users, etc.)    |  |
|  +------------------------------------------------------------+  |
+------------------------------------------------------------------+
```

---

## Startup & Initialization

Entry point: `cmd/mcpgw/main.go` -> `serveCommand()` -> `serveWithContext()`

The gateway initializes in the following order:

```
 1. config.Load()            Load MCPGW_* environment variables
         |
 2. newLogger()              Initialize structured logger (JSON/text)
         |
 3. initTracing()            Setup OpenTelemetry SDK + OTLP exporter
         |
 4. openPostgresDB()         Connect to PostgreSQL
         |
 5. initStores()             Create store implementations
         |                   (ServerStore, APIKeyStore, AuditStore,
         |                    ClassificationStore, UserStore, SynonymStore)
         |
 6. CapabilityIndex.Rebuild() Build in-memory tool/resource index
         |                    from active servers in DB
         |
 7. server.New()             Create HTTP server with chi router
         |
 8. initRateLimitBackend()   Setup rate limiter (memory|dragonfly|nats)
         |
 9. registration.NewService() Create registration lifecycle service
         |
10. selectBroadcaster()      Setup cross-pod event broadcaster
         |
11. registration.NewSweeper() Start heartbeat sweeper goroutine
         |
12. initProxyServer()        Create proxy + MCP server + streamable HTTP
         |
13. initOrchestrator()       (optional) Wire LLM orchestrator meta-tool
         |
14. auth.AuthMiddleware()    Wire authentication middleware chain
         |
15. ServerScopeMiddleware    (if MCPGW_SERVER_SCOPE_ENFORCEMENT=true)
         |
16. Mount routes             Registration, proxy, admin, API key routes
         |
17. Per-server endpoints     (if MCPGW_PER_SERVER_ENDPOINTS=true)
         |                   VirtualServerHandler at /mcp/servers/{name}/
         |
18. Discovery routes         /.well-known/mcp.json + GET /mcp/servers
         |
19. gw.Start()               Listen on TLS :8443 + metrics on :9090
```

---

## Request Lifecycle

A complete tool-call request flows through the system as follows:

```
Client (IDE / LLM Agent)
  |
  |  HTTPS POST /mcp/ or /mcp/servers/{name}/ (Streamable HTTP or SSE)
  v
+-------------------------------------------------------------------+
| 1. RequestID Middleware       Assign X-Request-ID correlation ID   |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 2. Tracing Middleware         Start OpenTelemetry span             |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 3. Metrics Middleware         Increment request counter, start     |
|                               latency timer                       |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 4. Logging Middleware         Log method, path, client info        |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 5. Auth Middleware            Authenticate via one of:             |
|    +-- mTLS?  --> Extract SPIFFE ID from client cert              |
|    +-- JWT?   --> Validate via OIDC issuer, cache token           |
|    +-- APIKey? -> SHA-256 lookup + bcrypt verify                  |
|    Result: Identity + ServerScope injected into context            |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 5b. Server Scope Middleware   For per-server endpoints:            |
|     (if enabled)              check {serverName} against scope.    |
|                               For unified endpoint: set scope in   |
|                               context for deferred check.          |
|                               Empty scope = gateway-wide access.   |
|                               403 + X-Denied-Reason if denied.     |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 6. Rate Limit Middleware      Resolve client profile               |
|                               Token-bucket check via backend       |
|                               429 if exceeded + Retry-After header |
+-------------------------------------------------------------------+
  |
+-------------------------------------------------------------------+
| 7. Policy Middleware          Check tool against allow/deny list   |
|                               Validate risk classification         |
|                               Scan args for dangerous patterns     |
|                               403 if denied                        |
+-------------------------------------------------------------------+
  |
  +-------- /mcp/servers/{name}/ --------+-------- /mcp/ (unified) --------+
  |                                      |                                  |
+-------------------------------+  +-------------------------------------------+
| 8a. VirtualServerHandler      |  | 8b. ProxyServer Handler                   |
|     Resolve {serverName}      |  |     a. Parse tool name + arguments         |
|     Validate name pattern     |  |     b. ToolCache lookup                    |
|     Lookup in CapabilityIndex |  |     c. NamespaceResolver.Resolve(name)     |
|     404 if not found          |  |     d. CapabilityIndex.FindServers(tool)   |
|     503 if not active         |  |     e. Server scope check (deferred)       |
|     Reverse proxy to backend  |  |     f. Router.Route() -> strategy selects  |
|     (bare tool names, no      |  |        (RoundRobin or SessionAffinity)     |
|      namespace prefix)        |  |     g. Circuit breaker + retry             |
+-------------------------------+  |     h. BackendClient.CallTool()            |
                                   |     i. Audit log insert                    |
                                   +-------------------------------------------+
  |
  v
Client receives JSON / SSE response
```

---

## Authentication & Authorization

The gateway supports three authentication methods, evaluated in priority order.

```
Incoming Request
  |
  +-- Has verified TLS client cert?
  |     YES --> Extract SPIFFE ID from SAN
  |             Validate against MCPGW_SPIFFE_ALLOW_PREFIX
  |             Scope: Admin
  |
  +-- Has Bearer token that looks like JWT?
  |     YES --> Validate signature via OIDC issuer
  |             Check audience claim
  |             Cache validated token (TTL-based)
  |             Auto-register user if admin mode enabled
  |             Scope: from OIDC claims + user role
  |
  +-- Has Bearer token or X-API-Key header?
        YES --> SHA-256 hash for fast lookup in api_keys table
                bcrypt verify against stored hash
                Resolve policy profile from api_key record
                Scope: from api_key.scopes
```

### RBAC Model

| Role     | Permissions                                    |
|----------|------------------------------------------------|
| `admin`  | All tools, all admin endpoints, user management |
| `user`   | Default policy profile, per-user tool overrides |
| `viewer` | Read-only tools only                            |
| `blocked`| No access                                       |

### Per-User Tool Permissions

```
gateway_users (1) ---< (N) user_tool_permissions
     |                        |
     +-- role (base access)   +-- tool_name (explicit grant)
     +-- oidc_sub             +-- granted_by (audit trail)
```

### Server-Scoped API Keys

API keys can optionally restrict access to specific backend servers via the `server_scope` field. When enforcement is enabled (`MCPGW_SERVER_SCOPE_ENFORCEMENT=true`), the `ServerScopeMiddleware` checks every request:

```
Auth Middleware → identity.ServerScope = ["todoist", "github"]
       |
ServerScopeMiddleware
       |
  Per-server endpoint (/mcp/servers/slack/)?
       |
  "slack" in ["todoist", "github"]? → NO → 403 Forbidden
                                            X-Denied-Reason: server_scope
```

For the unified `/mcp/` endpoint, scope checking is deferred until after tool-to-server resolution in the Router, since the target server is not known until the tool name is resolved via the CapabilityIndex.

An empty `server_scope` means gateway-wide access (backward compatible with existing keys).

Implementation files:
- `internal/auth/apikey_middleware.go` - API key authentication + server scope injection
- `internal/auth/oidc_middleware.go` - OIDC JWT validation
- `internal/auth/server_scope.go` - ServerScopeMiddleware, CheckServerScope
- `internal/identity/middleware.go` - mTLS / SPIFFE extraction
- `internal/auth/rbac.go` - Role-based access control

---

## Backend Server Management

Backend MCP servers register with the gateway and maintain their presence via heartbeats.

### Server State Machine

```
                  +---------+
                  | pending |  <-- POST /registration
                  +----+----+
                       |
                  (admin approval)
                       |
                  +----v----+
                  | approved|
                  +----+----+
                       |
                  (first heartbeat)
                       |
                  +----v----+
          +------>| active  | <--+
          |       +----+----+    |
          |            |         |
     (heartbeat)  (no heartbeat  |
          |        for TTL)      |
          |            |         |
          |       +----v----+    |
          +-------+  stale  +----+
                  +----+----+  (heartbeat
                       |        resumes)
                  (TTL exceeded)
                       |
                  +----v----+
                  | expired |  --> Removed from index
                  +---------+
```

### Registration Flow

1. Backend sends `POST /registration` with name, version, host, port, and declared capabilities (tools/resources/prompts)
2. Gateway validates the mTLS SPIFFE identity
3. Creates a `ServerRecord` with `status=pending`
4. On approval, status transitions to `approved`
5. Backend begins heartbeat loop: `POST /registration/{id}/heartbeat`
6. First heartbeat transitions to `active`; server added to `CapabilityIndex`
7. `Sweeper` goroutine runs every 10s, marks stale/expired servers

### Capability Index

The `CapabilityIndex` is an in-memory, mutex-protected map that enables O(1) tool-to-server lookups:

```
CapabilityIndex
  tools:     map[toolName] -> []ServerRecord
  resources: map[resourceURI] -> []ServerRecord

  LookupTool(name) -> []ServerRecord
  LookupServer(name) -> *ServerRecord    (case-insensitive, scans tool entries)
  ListServers() -> []ServerStats          (deduplicated, with tool/resource counts)
```

It is rebuilt from the database on startup and kept in sync via heartbeats, registrations, capability refresh events, and cross-pod broadcasts.

### Heartbeat Capability Refresh

Each backend includes a `capabilities_hash` (SHA-256 of its serialized tool/resource catalog) in heartbeat payloads. The gateway compares this hash to the stored value and triggers an inline capability refresh on mismatch:

```
Backend                           Gateway
  |                                 |
  |-- POST /registration/{id}/heartbeat
  |   { "capabilities_hash": "a1b2c3..." }
  |                                 |
  |                    Compare hash with stored value
  |                    Hash matches? → normal heartbeat response
  |                    Hash differs? → call tools/list on backend
  |                                   → update CapabilityIndex
  |                                   → trigger incremental search reindex
  |                                   → store new hash
  |                                   → broadcast to other pods
  |                                 |
  |<-- 200 OK                       |
  |   { "capabilities_hash": "a1b2c3..." }
```

Backends that do not send the hash (e.g., third-party servers) fall back to existing behavior with no refresh triggered. Refresh failures are logged but never fail the heartbeat.

Implementation files:
- `internal/registration/index.go` - CapabilityIndex, LookupServer, ListServers
- `internal/registration/service.go` - Registration lifecycle, ToolLister integration
- `internal/registration/heartbeat.go` - Heartbeat with hash comparison
- `internal/registration/refresh.go` - RefreshCapabilities, ToolLister interface
- `internal/registration/refresh_handler.go` - Push refresh HTTP handler
- `internal/registration/sweeper.go` - Heartbeat sweeper
- `internal/registration/handler.go` - HTTP handlers

---

## Proxy & Routing

### Router

The `Router` selects a backend server for each tool call:

```
Router.Route(ctx, toolName, args)
  |
  1. NamespaceResolver.Resolve(toolName) (if configured)
  |     -> (serverHint, bareToolName) or fallback to federated name
  |
  2. CapabilityIndex.FindServers(toolName)
  |     -> []ServerRecord (all backends hosting this tool)
  |
  3. Server scope check (deferred from middleware)
  |     -> ErrServerScopeDenied if target server not in scope
  |
  4. RoutingStrategy.Select(ctx, candidates, req)
  |     -> RoundRobinStrategy: atomic counter % len(candidates)
  |     -> SessionAffinityStrategy: rendezvous hash on Mcp-Session-Id
  |        (falls back to RoundRobin when no session ID present)
  |
  5. Per-server rate limit check
  |     -> ServerRateLimitError if exceeded
  |
  6. Get/create ResilientClient for selected server
  |     -> Circuit breaker + retry wrapper
  |
  7. ResilientClient.CallTool(ctx, toolName, args)
  |     -> Circuit breaker gate
  |     -> HTTP POST to backend
  |     -> Retry on transient errors
  |
  8. Return ToolResult or error
```

### Routing Strategies

The `RoutingStrategy` interface allows pluggable backend selection:

```go
type RoutingStrategy interface {
    Select(ctx context.Context, candidates []types.ServerRecord,
           req *RoutingRequest) (*types.ServerRecord, error)
}
```

| Strategy | Behavior | Use Case |
|----------|----------|----------|
| `RoundRobinStrategy` | Atomic counter modulo candidate count | Default; stateless backends |
| `SessionAffinityStrategy` | Rendezvous (HRW) hashing on `Mcp-Session-Id` | Stateful backends with session state |

Session affinity is configured per-server via the `routing_strategy` column in `mcp_servers` (default: `round_robin`). When set to `session_affinity`, the gateway uses rendezvous hashing (FNV-1a 64-bit) to consistently map the same MCP session to the same backend replica. On replica removal, only sessions pinned to the removed replica are redistributed.

### Tool Namespace Isolation

The `NamespaceResolver` handles tool name collisions when multiple backends register tools with the same name:

| Mode | Config Value | Behavior |
|------|-------------|----------|
| Reject | `reject` (default) | Registration fails on tool name conflicts |
| Always namespace | `namespace_always` | All tools prefixed: `{server}.{tool}` on unified endpoint |
| Namespace on conflict | `namespace_on_conflict` | Only conflicting names prefixed; unique names stay bare |

On per-server endpoints (`/mcp/servers/{name}/`), tool names are always bare — no namespace prefix is applied since the scope is implicit.

The separator character is configurable via `MCPGW_NAMESPACE_SEPARATOR` (default: `.`). Resolution splits on the first separator to extract a server hint and bare tool name.

### Tool Cache

`ToolCache` stores tool definitions with a configurable TTL to avoid repeated `tools/list` calls to backends:

```
ToolCache
  entries: map[cacheKey] -> {definition, expiry, serverID, serverName}
  TTL: 30s default
  InvalidateServer(serverID)   per-server invalidation on capability refresh
```

### Streamable HTTP Transport

The gateway uses the `mark3labs/mcp-go` library's `StreamableHTTPServer` for full MCP protocol compliance, supporting bidirectional JSON-RPC over HTTP with SSE streaming for long-running operations.

Implementation files:
- `internal/proxy/server.go` - ProxyServer, namespace resolver wiring
- `internal/proxy/router.go` - Router, RoutingStrategy, RoutingRequest, server scope check
- `internal/proxy/session_affinity.go` - SessionAffinityStrategy (rendezvous hashing)
- `internal/proxy/namespace.go` - NamespaceResolver, namespace modes
- `internal/proxy/client.go` - BackendClient
- `internal/proxy/tools.go` - Tool aggregation, federated naming, namespace application
- `internal/proxy/cache.go` - ToolCache

---

## Per-Server Virtual Endpoints

When `MCPGW_PER_SERVER_ENDPOINTS=true` (default), each registered active backend is exposed as an individual, fully MCP-compliant endpoint:

```
Routes:
  GET|POST  /mcp/servers/{serverName}/       Full MCP passthrough
  GET|POST  /mcp/servers/{serverName}/sse    SSE transport variant
  GET       /.well-known/mcp.json            Auto-discovery document
  GET       /mcp/servers                     Server listing (JSON)
```

### VirtualServerHandler

The `VirtualServerHandler` resolves `{serverName}` from the URL, validates it against a strict pattern (`^[a-z0-9][a-z0-9_-]{0,62}$`), looks up the server in the CapabilityIndex, and reverse-proxies the request to the backend:

```
Client POST /mcp/servers/todoist/
  |
  v
Middleware Chain (same as unified: Auth -> ServerScope -> RateLimit -> Policy)
  |
  v
VirtualServerHandler
  |
  1. Extract {serverName} from URL
  2. Validate name pattern
  3. CapabilityIndex.LookupServer(name)
  4. Not found → 404 | Not active → 503
  5. Get or create cached reverse proxy instance (sync.Map)
  6. Forward request to backend at {scheme}://{host}:{port}/mcp
  |
  v
Client receives MCP JSON-RPC / SSE response
```

Per-server instances are cached in a `sync.Map` and invalidated when the server deregisters or its tool catalog changes.

### Auto-Discovery Document

`GET /.well-known/mcp.json` returns a JSON document listing all active per-server endpoints:

```json
{
  "mcpServers": {
    "todoist": {
      "url": "https://gw.example.com/mcp/servers/todoist/",
      "name": "Todoist MCP Server",
      "version": "1.2.0",
      "status": "active"
    }
  }
}
```

The response is cached in-memory with a configurable TTL (`MCPGW_WELL_KNOWN_CACHE_TTL`, default 5s) using an `atomic.Pointer` for lock-free reads. The cache is invalidated on server registration/deregistration events.

### Server Listing

`GET /mcp/servers` returns a JSON array with per-server metadata:

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

Implementation files:
- `internal/proxy/virtual_server.go` - VirtualServerHandler, reverse proxy creation
- `internal/proxy/discovery_doc.go` - DiscoveryDocHandler, TTL cache
- `internal/proxy/server_list.go` - ServerListHandler, server metadata

---

## Resilience Layer

```
+--------------------------------------------------+
|              ResilientClient                      |
|                                                   |
|  +------------------+    +--------------------+   |
|  | Circuit Breaker  |    | Retry Logic        |   |
|  |                  |    |                    |   |
|  | States:          |    | Strategy:          |   |
|  |  closed (normal) |    |  exponential       |   |
|  |  open (reject)   |    |  backoff           |   |
|  |  half_open       |    |  100ms, 200ms,     |   |
|  |   (probe)        |    |  400ms, ...        |   |
|  |                  |    |                    |   |
|  | Config:          |    | Retryable:         |   |
|  |  threshold: 5    |    |  timeout, 502,     |   |
|  |  timeout: 30s    |    |  503, 504          |   |
|  +------------------+    |                    |   |
|                          | Non-retryable:     |   |
|  +------------------+    |  400, 401, 403,    |   |
|  | Connection Pool  |    |  404               |   |
|  | (HTTP transport  |    +--------------------+   |
|  |  with keep-alive)|                             |
|  +------------------+                             |
+--------------------------------------------------+
```

### Circuit Breaker State Machine

```
       success          failure (count < threshold)
    +----------+       +----------+
    |          |       |          |
    v          |       v          |
+--------+    |    +--------+    |
| CLOSED |----+    | CLOSED |----+
+---+----+         +---+----+
    |                   |
    | failure count     |
    | >= threshold      |
    v                   |
+--------+              |
|  OPEN  |              |
+---+----+              |
    |                   |
    | timeout expires   |
    v                   |
+-----------+           |
| HALF_OPEN +-----------+
+-----+-----+  (probe succeeds -> CLOSED)
      |
      | (probe fails -> OPEN)
      v
  +--------+
  |  OPEN  |
  +--------+
```

### Breaker Registry

Each backend server gets its own circuit breaker, managed by `BreakerRegistry`:

```go
BreakerRegistry
  breakers: map[serverID] -> *CircuitBreaker
```

Configuration:
- `MCPGW_CIRCUIT_THRESHOLD` - Failures before open (default: 5)
- `MCPGW_CIRCUIT_TIMEOUT` - Recovery timeout (default: 30s)
- `MCPGW_RETRY_MAX` - Max retry attempts (default: 3)

Implementation files:
- `internal/resilience/circuit.go` - CircuitBreaker, BreakerRegistry
- `internal/resilience/retry.go` - Retry logic
- `internal/resilience/pool.go` - Connection pooling
- `internal/resilience/client.go` - ResilientClient

---

## Rate Limiting

Rate limiting uses a token-bucket algorithm with pluggable backends.

```
+-------------------+
| Rate Limit        |
| Middleware         |
+--------+----------+
         |
         v
  LimiterBackend.Allow(key, limit, burst)
         |
    +----+----+----------+
    |         |          |
    v         v          v
+--------+ +--------+ +------+
| Memory | |Dragonfly| | NATS |
| (local)| |(Redis-  | | (KV  |
|        | |compat.) | | Store)|
+--------+ +--------+ +------+
    |         |          |
    |    +----+----+     |
    |    | Fallback|     |
    |    | (Memory)|     |
    |    +---------+     |
    |                    |
    +-----+--------------+
          v
  Response: allow/deny + remaining + retryAfter
```

### Backend Selection

| Backend | Use Case | Config |
|---------|----------|--------|
| `memory` | Single-node / dev | Default |
| `dragonfly` | Multi-node, Redis-compatible | `MCPGW_DRAGONFLY_URL` |
| `nats` | Multi-node via NATS JetStream KV | `MCPGW_NATS_URL` |

Both `dragonfly` and `nats` backends include an automatic memory fallback if the distributed backend becomes unavailable.

### Rate Limit Resolution

```
Per-API Key:   api_keys.policy_profile -> policy_profiles.rate_limit
Per-OIDC User: gateway_users.role -> default profile
Per-Server:    mcp_servers.rate_limit / rate_burst (per-backend)
Global:        MCPGW_RATE_DEFAULT / MCPGW_RATE_BURST
```

Response headers follow RFC 6585:
- `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`, `Retry-After` (on 429)

Implementation files:
- `internal/ratelimit/backend.go` - LimiterBackend interface
- `internal/ratelimit/memory_backend.go` - In-process limiter
- `internal/ratelimit/dragonfly_backend.go` - DragonflyDB limiter
- `internal/ratelimit/nats_backend.go` - NATS JetStream limiter

---

## Policy Engine & Tool Classification

### Tool Classification

Every tool is assigned a risk level, either automatically from name/description heuristics or manually by an admin:

```
Tool Name Tokens
  |
  +-- Match destructive keywords?  -> risk_level: destructive
  |   (delete, remove, destroy,       (stored in tool_classifications)
  |    drop, purge, kill, ...)
  |
  +-- Match read-only keywords?    -> risk_level: read_only
  |   (get, list, read, describe,
  |    show, fetch, query, ...)
  |
  +-- Match write keywords?        -> risk_level: write
  |   (create, update, set, put,
  |    post, write, insert, ...)
  |
  +-- No match                     -> risk_level: unknown
```

### Policy Enforcement

```
Policy Middleware
  |
  1. Resolve policy profile for client identity
  |
  2. Check tool_name against profile.tool_allowlist
  |     -> Not in list? DENY
  |
  3. Check tool_name against profile.tool_denylist
  |     -> In list? DENY
  |
  4. Look up tool_classifications.risk_level
  |     -> destructive + non-admin? DENY (or AUDIT)
  |
  5. Scan string arguments for dangerous patterns
  |     -> SQL injection, path traversal, etc.
  |
  6. enforcement_mode == "audit"?
  |     YES -> Log violation, ALLOW
  |     NO  -> DENY with 403
  |
  7. Publish policy.violated event
```

Implementation files:
- `internal/policy/classification.go` - Auto-classification heuristics
- `internal/policy/engine.go` - Policy evaluation engine
- `internal/server/policy_middleware.go` - HTTP middleware

---

## Progressive Discovery & Search

When `MCPGW_PROGRESSIVE_DISCOVERY=true`, the gateway exposes meta-tools instead of the full tool catalog, reducing token usage for LLM clients.

```
Client                         Gateway                        Backends
  |                              |                               |
  |-- tools/list --------------->|                               |
  |<-- [cruvero.search,          |                               |
  |     cruvero.get_tools,       |                               |
  |     cruvero.orchestrate] ----|                               |
  |                              |                               |
  |-- cruvero.search("email") -->|                               |
  |                              |-- Search Engine Query ------->|
  |                              |   (BM25 / Vector / Hybrid)    |
  |<-- [{name: "send_email",     |                               |
  |      score: 0.92}, ...] -----|                               |
  |                              |                               |
  |-- cruvero.get_tools -------->|                               |
  |   (["send_email"])           |-- Fetch definitions --------->|
  |<-- [full tool definitions] --|                               |
  |                              |                               |
  |-- tools/call "send_email" -->|                               |
  |                              |-- Route + proxy to backend -->|
  |<-- result -------------------|<-- result --------------------|
```

### Search Engine Options

```
+------------------------------------------------------------------+
|                    Search Engines                                  |
|                                                                   |
|  +--------------+  +--------------+  +-------------------------+  |
|  | Substring    |  | BM25         |  | Vector                  |  |
|  | (pg_trgm)   |  | (in-memory   |  | (ONNX Runtime           |  |
|  |              |  |  inverted    |  |  embeddings +           |  |
|  |              |  |  index)      |  |  cosine similarity)     |  |
|  +--------------+  +--------------+  +-------------------------+  |
|                                                                   |
|  +-------------------------------------------------------------+  |
|  | Hybrid = BM25 + Vector (reciprocal rank fusion)             |  |
|  +-------------------------------------------------------------+  |
+------------------------------------------------------------------+
```

| Engine | Config Value | Description |
|--------|-------------|-------------|
| Substring | `substring` | PostgreSQL trigram matching |
| BM25 | `bm25` | In-memory partitioned inverted index with TF-IDF scoring |
| Vector | `vector` | ONNX model embeddings with cosine similarity |
| Hybrid | `hybrid` | BM25 + Vector with reciprocal rank fusion |

### Partitioned Search Indexes

Both BM25 and Vector engines use server-partitioned indexes for incremental updates. When a backend's tool catalog changes (detected via heartbeat hash mismatch or push refresh), only that server's partition is rebuilt — not the entire index.

```
BM25Engine
  partitions: map[serverID] -> *bm25Partition
  globalIDF:  map[term] -> float64  (recomputed lazily after partition change)

  AddPartition(serverID, docs)    Insert server's tools
  UpdatePartition(serverID, docs) Replace server's tools
  RemovePartition(serverID)       Delete server's tools
  Index(docs)                     Full rebuild (backward compat)
  Search(query, limit)            Query across all partitions

VectorEngine
  partitions: map[serverID] -> *vectorPartition
  cache:      *EmbeddingCache     (LRU, keyed by content hash)

  AddPartition(ctx, serverID, docs)
  UpdatePartition(ctx, serverID, docs)   Reuse cached embeddings for unchanged tools
  RemovePartition(serverID)

HybridEngine
  UpdatePartition(ctx, serverID, docs)   Delegates to both BM25 and Vector
  RemovePartition(serverID)              Delegates to both
```

### Embedding Cache

The `EmbeddingCache` avoids recomputing ONNX embeddings for unchanged tools during incremental reindex:

```
EmbeddingCache
  entries: map[SHA-256(name + description)] -> []float32
  maxSize: MCPGW_EMBEDDING_CACHE_MAX_SIZE (default: 10000)
  eviction: LRU
```

When a server's tools change, content hashes are computed for each tool. Tools whose hash matches the cache reuse the existing embedding. Only new or changed tools trigger ONNX inference.

Implementation files:
- `internal/search/engine.go` - Engine interface
- `internal/search/bm25.go` - BM25 engine (partitioned)
- `internal/search/vector.go` - Vector search engine (partitioned)
- `internal/search/hybrid.go` - Hybrid fusion engine (partition delegation)
- `internal/search/embedding_cache.go` - LRU embedding cache
- `internal/search/onnx_embedder.go` - ONNX Runtime embedder
- `internal/proxy/discovery.go` - DiscoveryIndex, meta-tool handlers

---

## Realtime Capability Refresh

Two complementary mechanisms ensure the CapabilityIndex and search indexes always reflect the current tool catalog:

### Mechanism 1 — Heartbeat Hash Detection

Every heartbeat can include a `capabilities_hash`. The gateway compares it to the stored hash. On mismatch, the gateway triggers an inline `tools/list` refresh, updates the store and indexes, and broadcasts the change to other pods. See [Heartbeat Capability Refresh](#heartbeat-capability-refresh) above.

### Mechanism 2 — Push Refresh Endpoint

Fleet servers can proactively notify the gateway when their capabilities change:

```
POST /v1/registrations/{id}/capabilities
Authorization: mTLS (SPIFFE ID must match registered server)
Rate Limit: 10 requests/minute per server

Response: 200 OK
{
  "tools_count": 12,
  "resources_count": 3,
  "reindex_triggered": true,
  "capabilities_hash": "d4e5f6..."
}
```

The handler validates the caller's SPIFFE identity, enforces per-server rate limiting, calls `RefreshCapabilities()` to fetch the updated catalog, and broadcasts a `server.capabilities_changed` event for cross-pod synchronization.

### Refresh Pipeline

```
Hash mismatch OR Push endpoint
  |
  v
RefreshCapabilities()
  |
  1. ToolLister.ListToolNames(server)   Fetch current tools from backend
  2. ServerStore.Update(record)          Persist new hash + capabilities
  3. CapabilityIndex.RefreshServer()     Update in-memory index
  4. Broadcaster.Publish(event)          Notify other gateway pods
  5. ToolCache.InvalidateServer(id)      Clear cached tool definitions
```

Implementation files:
- `internal/registration/refresh.go` - RefreshCapabilities, ToolLister interface
- `internal/registration/refresh_handler.go` - Push endpoint HTTP handler
- `internal/registration/heartbeat.go` - Hash comparison integration

---

## Orchestrator

When `MCPGW_ORCHESTRATE_ENABLED=true`, the gateway exposes a `cruvero.orchestrate` meta-tool that performs LLM-driven multi-step tool planning and execution.

```
Client
  |
  |-- cruvero.orchestrate
  |   { intent: "Send weekly report email",
  |     mode: "execute",
  |     safety: "strict" }
  |
  v
+-------------------------------+
| Orchestrator                  |
|                               |
| 1. Discover available tools   |
|    via ToolDiscoverer         |
|                               |
| 2. Send intent + tool list    |
|    to LLM (OpenAI-compatible) |
|                               |
| 3. LLM returns plan:         |
|    Step 1: get_report_data    |
|    Step 2: format_email       |
|    Step 3: send_email         |
|    (max 10 steps)             |
|                               |
| 4. mode == "plan"?            |
|    YES -> Return plan only    |
|    NO  -> Execute each step:  |
|           ToolExecutor.Call() |
|           Audit each result   |
|           Enforce safety      |
|                               |
| 5. Return aggregated result   |
+-------------------------------+
```

Modes:
- **plan** - Dry-run; returns the planned steps without executing
- **execute** - Plans and executes each step sequentially

Safety levels:
- **strict** - Full tool access within policy constraints
- **read_only** - Only `read_only`-classified tools may be called

Implementation files:
- `internal/orchestrator/orchestrator.go` - Core orchestration logic
- `internal/orchestrator/types.go` - Request/result types
- `internal/llm/client.go` - LLM client interface
- `internal/llm/openai.go` - OpenAI-compatible implementation

---

## Events & Cross-Pod Synchronization

The events system enables multi-pod coordination and external monitoring.

```
+---------------+                +---------------+
| Gateway Pod A |                | Gateway Pod B |
|               |   NATS/        |               |
| Publisher ----+-> JetStream <--+-- Subscriber  |
| Subscriber <--+-- or         --+-> Publisher    |
|               |   Dragonfly    |               |
+-------+-------+   PubSub      +-------+-------+
        |                                |
        +--------- shared topics --------+

Topics / Subjects:
  {gateway_id}.server.registered
  {gateway_id}.server.deregistered
  {gateway_id}.server.health_changed
  {gateway_id}.server.capabilities_changed
  {gateway_id}.tool.called
  {gateway_id}.policy.violated
  {gateway_id}.search.fallback
  {gateway_id}.tool_cache.invalidate
```

### Broadcast Use Cases

| Event | Trigger | Subscriber Action |
|-------|---------|-------------------|
| `server.registered` | New backend approved | Rebuild CapabilityIndex on other pods |
| `server.deregistered` | Backend removed | Remove from index on other pods |
| `server.health_changed` | Heartbeat timeout | Update server status across cluster |
| `server.capabilities_changed` | Tool catalog changed | Refresh CapabilityIndex + search indexes on all pods |
| `tool_cache.invalidate` | Tool definitions changed | Clear ToolCache on all pods |
| `policy.violated` | Blocked request | Audit trail, monitoring alerts |

Implementation files:
- `internal/events/publisher.go` - Event publishing
- `internal/events/subscriber.go` - Event subscription
- `internal/events/client.go` - NATS client
- `internal/events/degradation.go` - Graceful degradation tracking

---

## Admin Dashboard

When `MCPGW_ADMIN_ENABLED=true`, an HTMX-powered web UI is served at `/admin/`.

```
/admin/
  |
  +-- /admin/dashboard      Overview: server count, tool count, health
  +-- /admin/servers         List/inspect/approve/deregister backends
  +-- /admin/tools           Tool classification review and editing
  +-- /admin/apikeys         API key management (create/revoke)
  +-- /admin/users           User management and role assignment
  +-- /admin/audit           Audit log viewer with filters
  +-- /admin/ratelimits      Rate limit configuration per-server
  +-- /admin/search          Search engine status and reindex controls
  +-- /admin/settings        Gateway configuration viewer
```

Admin authentication uses OIDC with session cookies (AES-GCM encrypted, CSRF-protected).

Admin modes:
- **standalone** - Gateway manages its own users and permissions
- **delegated** - User management deferred to external Cruvero platform

Implementation: `internal/admin/`

---

## Observability

### Metrics (Prometheus)

Exposed on `:9090/metrics`:

```
Gateway Metrics
  |
  +-- gateway_http_requests_total          {method, path, status}
  +-- gateway_http_request_duration_seconds {method, path}   [histogram]
  +-- gateway_active_connections                              [gauge]
  |
  +-- gateway_tool_calls_total             {tool, server}
  +-- gateway_tool_call_duration_seconds   {tool, server}    [histogram]
  +-- gateway_tool_errors_total            {tool, error_type}
  |
  +-- gateway_rate_limited_total           {client}
  +-- gateway_rate_limit_remaining         {client}          [gauge]
  |
  +-- gateway_circuit_breaker_state        {server}          [gauge]
  +-- gateway_circuit_breaker_trips_total  {server}
  |
  +-- gateway_policy_violations_total      {type}
  +-- gateway_policy_enforced_total        {action}
  |
  +-- gateway_search_requests_total
  +-- gateway_search_fallbacks_total
```

### Tracing (OpenTelemetry)

Traces are exported via OTLP to `OTEL_EXPORTER_OTLP_ENDPOINT`. Instrumented spans include HTTP requests (inbound/outbound), database queries, tool calls, policy evaluation, and rate limit checks.

### Structured Logging

JSON or text format (`MCPGW_LOG_FORMAT`), with contextual fields: `request_id`, `client_id`, `tool_name`, `server_name`, `auth_type`, `duration`.

### Health Endpoints

| Endpoint | Purpose | Checks |
|----------|---------|--------|
| `GET /healthz` | Liveness probe | Process alive |
| `GET /readyz` | Readiness probe | DB connected, search ready, NATS connected |

Implementation: `internal/server/metrics.go`, `internal/server/health.go`, `internal/server/tracing.go`

---

## Database Schema

PostgreSQL is the sole required data dependency. Schema managed via numbered SQL migrations in `migrations/`.

```
+---------------------+       +---------------------+
| mcp_servers         |       | api_keys            |
|---------------------|       |---------------------|
| id (UUID, PK)       |       | id (UUID, PK)       |
| name                |       | key_lookup_hash      |
| spiffe_id (UNIQUE)  |       | key_bcrypt_hash      |
| version             |       | name                 |
| host                |       | scopes (TEXT[])      |
| port                |       | client_id            |
| capabilities (JSONB)|       | policy_profile       |
| status              |       | server_scope (TEXT[])|
| policy_profile      |       | expires_at           |
| protocol            |       | created_at           |
| lease_epoch         |       +---------------------+
| capability_hash     |
| routing_strategy    |       +---------------------+
| rate_limit          |       | audit_log            |
| rate_burst          |       |---------------------|
| last_heartbeat      |       | id (UUID, PK)       |
| created_at          |       | event_type           |
| updated_at          |       | client_id            |
+---------------------+       |
                              | server_name          |
+---------------------+       | username             |
| gateway_users       |       | details (JSONB)      |
|---------------------|       | created_at           |
| id (TEXT, PK)       |       +---------------------+
| oidc_sub (UNIQUE)   |
| email               |       +---------------------+
| display_name        |       | tool_classifications |
| role                |       |---------------------|
| created_at          |       | tool_name (PK)      |
| updated_at          |       | risk_level           |
+--------+------------+       | reason               |
         |                    | auto_classified      |
         | 1:N                | updated_by           |
         v                    | updated_at           |
+---------------------+       +---------------------+
| user_tool_permissions|
|---------------------|       +---------------------+
| user_id (FK)        |       | config_cache         |
| tool_name           |       |---------------------|
| granted_by          |       | key (PK)             |
| granted_at          |       | value (JSONB)        |
+---------------------+       | updated_at           |
                              +---------------------+
```

Migration history (17 migrations):
1. `mcp_servers` - Core server registration table
2. `api_keys` - API key storage with hash-based lookup
3. `audit_log` - Immutable audit trail
4. `config_cache` - Key-value config persistence
5. `server_protocol` - Add protocol field to servers
6. `registration_leases` - Lease epoch, capability_hash, sync_state
7. `audit_log_retention_index` - Index for retention cleanup
8. `apikey_policy_profile` - Policy profile per API key
9. `tool_classifications` - Tool risk classification table
10. `server_rate_limit` - Per-server rate limit columns
11. `pg_trgm_search_indexes` - Trigram indexes for search
12. `user_access` - Users and per-user tool permissions
13. `search_config` - Search engine configuration tables
14. `audit_log_username_and_sort_support` - Audit log enhancements
15. `server_capabilities_hash` - Ensure capabilities_hash column exists
16. `apikey_server_scope` - Server-scope restrictions on API keys
17. `server_routing_strategy` - Per-server routing strategy column

---

## Kubernetes Deployment

The Helm chart at `charts/mcpgateway/` deploys the full gateway stack.

```
+------------------------------------------------------------------+
|  Kubernetes Cluster                                               |
|                                                                   |
|  +---------------------------+  +-----------------------------+   |
|  | Deployment: mcpgateway    |  | Service: mcpgateway         |   |
|  | replicas: 2-10 (HPA)     |  | :8443 (HTTPS) + :9090       |   |
|  |                           |  +-----------------------------+   |
|  | Init Container:           |                                    |
|  |   mcpgw migrate up        |  +-----------------------------+   |
|  |                           |  | Ingress                     |   |
|  | Main Container:           |  | TLS termination at gateway  |   |
|  |   mcpgw serve             |  +-----------------------------+   |
|  |                           |                                    |
|  | Security:                 |  +-----------------------------+   |
|  |   runAsNonRoot: true      |  | ServiceMonitor              |   |
|  |   runAsUser: 65534        |  | Scrape :9090/metrics        |   |
|  +---------------------------+  +-----------------------------+   |
|                                                                   |
|  +---------------------------+  +-----------------------------+   |
|  | HPA                       |  | PDB                         |   |
|  | min: 2, max: 10           |  | minAvailable: 1             |   |
|  | target: 70% CPU           |  +-----------------------------+   |
|  +---------------------------+                                    |
|                                 +-----------------------------+   |
|  +---------------------------+  | NetworkPolicy               |   |
|  | cert-manager Certificate  |  | Restrict ingress/egress     |   |
|  | Auto-rotate TLS certs     |  +-----------------------------+   |
|  +---------------------------+                                    |
|                                 +-----------------------------+   |
|  +---------------------------+  | PrometheusRule              |   |
|  | Vault Secrets             |  | Alert rules for SLOs        |   |
|  | DB URL, OIDC secret,      |  +-----------------------------+   |
|  | TLS keys                  |                                    |
|  +---------------------------+  +-----------------------------+   |
|                                 | OTEL Collector (sidecar)    |   |
|                                 | Trace export                |   |
|                                 +-----------------------------+   |
+------------------------------------------------------------------+
```

Helm values allow per-environment overlays (dev, staging, prod) for replica counts, resource limits, feature flags, and secrets.

---

## CLI Subcommands

The `mcpgw` binary serves as both the gateway server and an operational CLI:

| Command | Description |
|---------|-------------|
| `mcpgw serve` | Start the gateway server (default) |
| `mcpgw server list` | List registered backend servers |
| `mcpgw server inspect <id>` | Show server details and capabilities |
| `mcpgw server deregister <id>` | Remove a backend server |
| `mcpgw apikey list` | List API keys |
| `mcpgw apikey create` | Generate a new API key (supports `--server-scope`) |
| `mcpgw apikey revoke <id>` | Revoke an API key |
| `mcpgw policy list` | List policy profiles |
| `mcpgw health` | Check gateway liveness and readiness |
| `mcpgw tool classify` | Auto-classify tools by risk level |
| `mcpgw auth device` | Initiate OAuth2 device flow login |
| `mcpgw migrate up\|down` | Run database migrations |
| `mcpgw mcp-proxy` | stdio-to-HTTP bridge for IDE integration |

---

## Package Map

```
cmd/mcpgw/
  main.go                    Entry point and subcommand dispatch
  serve.go                   Gateway server initialization
  proxy.go                   stdio-to-HTTP MCP proxy bridge
  server.go, apikey.go, ...  CLI subcommand handlers

internal/
  admin/                     HTMX admin dashboard and API handlers
  auth/                      Authentication middleware (mTLS, OIDC, API key, server scope)
  config/                    MCPGW_* environment variable loading
  events/                    NATS/Dragonfly event publishing and subscription
  identity/                  Identity context propagation and SPIFFE extraction
  llm/                       LLM client abstraction (OpenAI-compatible)
  orchestrator/              LLM-driven multi-step tool planning/execution
  policy/                    Policy evaluation engine and tool classification
  proxy/                     Core MCP proxy: routing, caching, discovery,
                              per-server endpoints, namespace resolution,
                              session affinity, discovery document
  ratelimit/                 Rate limiting backends (memory, Dragonfly, NATS)
  registration/              Server registration lifecycle, heartbeats,
                              capability refresh (hash detection + push)
  resilience/                Circuit breakers, retry logic, connection pooling
  search/                    Partitioned search engines (BM25, vector, hybrid),
                              LRU embedding cache
  server/                    HTTP server, middleware, health, metrics, tracing
  store/                     PostgreSQL store implementations
  types/                     Shared type definitions
  testutil/                  Test helpers and fixtures

migrations/                  PostgreSQL schema migrations (0001-0017)
charts/mcpgateway/           Helm chart for Kubernetes deployment
deploy/                      ArgoCD and PKI deployment configuration
```
