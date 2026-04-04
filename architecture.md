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
8. [Resilience Layer](#resilience-layer)
9. [Rate Limiting](#rate-limiting)
10. [Policy Engine & Tool Classification](#policy-engine--tool-classification)
11. [Progressive Discovery & Search](#progressive-discovery--search)
12. [Orchestrator](#orchestrator)
13. [Events & Cross-Pod Synchronization](#events--cross-pod-synchronization)
14. [Admin Dashboard](#admin-dashboard)
15. [Observability](#observability)
16. [Database Schema](#database-schema)
17. [Kubernetes Deployment](#kubernetes-deployment)
18. [CLI Subcommands](#cli-subcommands)
19. [Package Map](#package-map)

---

## Overview

The MCP Gateway is a production-grade reverse proxy and aggregator for [Model Context Protocol](https://modelcontextprotocol.io) (MCP) servers, written in Go. It sits between AI clients (IDEs, LLM agents) and one or more MCP backend servers, providing a single entry point with unified authentication, rate limiting, policy enforcement, tool discovery, and observability.

```
 +-----------+    +-----------+    +-----------+
 |  IDE /    |    |  LLM      |    |  CLI      |
 |  VS Code  |    |  Agent    |    |  Client   |
 +-----+-----+    +-----+-----+    +-----+-----+
       |                |                |
       +--------+-------+--------+-------+
                |   MCP Protocol (HTTPS)
                v
  +-----------------------------+
  |      MCP Gateway (:8443)    |
  |                             |
  |  Auth -> RateLimit -> Policy|
  |         -> Proxy -> Route   |
  +------+----------+-----------+
         |          |
    +----+----+ +---+-----+
    | Backend | | Backend  |  ...
    | MCP Srv | | MCP Srv  |
    +---------+ +----------+
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
|  |  -> Auth (mTLS | OIDC | API Key)                          |  |
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
|  |  +-------v-------+                                        |  |
|  |  | Router        |  +----------------+  +--------------+  |  |
|  |  | (round-robin, |  | Orchestrator   |  | Search       |  |  |
|  |  |  circuit      |  | (LLM-driven    |  | (BM25,       |  |  |
|  |  |  breaker,     |  |  multi-step    |  |  vector,     |  |  |
|  |  |  retry)       |  |  planning)     |  |  hybrid)     |  |  |
|  |  +-------+-------+  +----------------+  +--------------+  |  |
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
15. Mount routes             Registration, proxy, admin, API key routes
         |
16. gw.Start()               Listen on TLS :8443 + metrics on :9090
```

---

## Request Lifecycle

A complete tool-call request flows through the system as follows:

```
Client (IDE / LLM Agent)
  |
  |  HTTPS POST /mcp/ (Streamable HTTP or SSE)
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
|    Result: Identity injected into context                         |
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
+-------------------------------------------------------------------+
| 8. ProxyServer Handler                                            |
|    a. Parse tool name + arguments                                 |
|    b. ToolCache lookup (hit -> use cached definition)             |
|    c. CapabilityIndex.FindServers(toolName)                       |
|    d. Router.Route() -> RoundRobinStrategy selects backend        |
|    e. Circuit breaker check (open -> 503)                         |
|    f. BackendClient.CallTool() -> HTTP to backend MCP server      |
|    g. Retry on transient failure (exponential backoff)            |
|    h. Normalize response -> ToolResult                            |
|    i. Audit log insert                                            |
+-------------------------------------------------------------------+
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

Implementation files:
- `internal/auth/apikey_middleware.go` - API key authentication
- `internal/auth/oidc_middleware.go` - OIDC JWT validation
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
```

It is rebuilt from the database on startup and kept in sync via heartbeats, registrations, and cross-pod broadcasts.

Implementation files:
- `internal/registration/index.go` - CapabilityIndex
- `internal/registration/service.go` - Registration lifecycle
- `internal/registration/sweeper.go` - Heartbeat sweeper
- `internal/registration/handler.go` - HTTP handlers

---

## Proxy & Routing

### Router

The `Router` selects a backend server for each tool call:

```
Router.Route(ctx, toolName, args)
  |
  1. CapabilityIndex.FindServers(toolName)
  |     -> []ServerRecord (all backends hosting this tool)
  |
  2. RoutingStrategy.Select(candidates)
  |     -> RoundRobinStrategy: atomic counter % len(candidates)
  |
  3. Per-server rate limit check
  |     -> ServerRateLimitError if exceeded
  |
  4. Get/create ResilientClient for selected server
  |     -> Circuit breaker + retry wrapper
  |
  5. ResilientClient.CallTool(ctx, toolName, args)
  |     -> Circuit breaker gate
  |     -> HTTP POST to backend
  |     -> Retry on transient errors
  |
  6. Return ToolResult or error
```

### Tool Cache

`ToolCache` stores tool definitions with a configurable TTL to avoid repeated `tools/list` calls to backends:

```
ToolCache
  entries: map[cacheKey] -> {definition, expiry}
  TTL: 30s default
```

### Streamable HTTP Transport

The gateway uses the `mark3labs/mcp-go` library's `StreamableHTTPServer` for full MCP protocol compliance, supporting bidirectional JSON-RPC over HTTP with SSE streaming for long-running operations.

Implementation files:
- `internal/proxy/server.go` - ProxyServer
- `internal/proxy/router.go` - Router, RoutingStrategy, RoundRobinStrategy
- `internal/proxy/client.go` - BackendClient
- `internal/proxy/cache.go` - ToolCache

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
| BM25 | `bm25` | In-memory inverted index with TF-IDF scoring |
| Vector | `vector` | ONNX model embeddings with cosine similarity |
| Hybrid | `hybrid` | BM25 + Vector with reciprocal rank fusion |

Implementation files:
- `internal/search/engine.go` - Engine interface
- `internal/search/bm25.go` - BM25 engine
- `internal/search/vector.go` - Vector search engine
- `internal/search/hybrid.go` - Hybrid fusion engine
- `internal/search/onnx_embedder.go` - ONNX Runtime embedder
- `internal/proxy/discovery.go` - DiscoveryIndex, meta-tool handlers

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
| status              |       | expires_at           |
| policy_profile      |       | created_at           |
| protocol            |       +---------------------+
| lease_epoch         |
| rate_limit          |       +---------------------+
| rate_burst          |       | audit_log            |
| last_heartbeat      |       |---------------------|
| created_at          |       | id (UUID, PK)       |
| updated_at          |       | event_type           |
+---------------------+       | client_id            |
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

Migration history (14 migrations):
1. `mcp_servers` - Core server registration table
2. `api_keys` - API key storage with hash-based lookup
3. `audit_log` - Immutable audit trail
4. `config_cache` - Key-value config persistence
5. `server_protocol` - Add protocol field to servers
6. `registration_leases` - Lease epoch for consistency
7. `audit_log_retention_index` - Index for retention cleanup
8. `apikey_policy_profile` - Policy profile per API key
9. `tool_classifications` - Tool risk classification table
10. `server_rate_limit` - Per-server rate limit columns
11. `pg_trgm_search_indexes` - Trigram indexes for search
12. `user_access` - Users and per-user tool permissions
13. `search_config` - Search engine configuration tables
14. `audit_log_username_and_sort_support` - Audit log enhancements

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
| `mcpgw apikey create` | Generate a new API key |
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
  auth/                      Authentication middleware (mTLS, OIDC, API key)
  config/                    MCPGW_* environment variable loading
  events/                    NATS/Dragonfly event publishing and subscription
  identity/                  Identity context propagation and SPIFFE extraction
  llm/                       LLM client abstraction (OpenAI-compatible)
  orchestrator/              LLM-driven multi-step tool planning/execution
  policy/                    Policy evaluation engine and tool classification
  proxy/                     Core MCP proxy: routing, caching, discovery
  ratelimit/                 Rate limiting backends (memory, Dragonfly, NATS)
  registration/              Server registration lifecycle and heartbeats
  resilience/                Circuit breakers, retry logic, connection pooling
  search/                    Full-text search engines (BM25, vector, hybrid)
  server/                    HTTP server, middleware, health, metrics, tracing
  store/                     PostgreSQL store implementations
  types/                     Shared type definitions
  testutil/                  Test helpers and fixtures

migrations/                  PostgreSQL schema migrations (0001-0014)
charts/mcpgateway/           Helm chart for Kubernetes deployment
deploy/                      ArgoCD and PKI deployment configuration
```
