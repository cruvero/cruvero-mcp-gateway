# Feature Specification: Per-Server Endpoints, Realtime Capability Refresh & IDE Integration

**Feature Branch**: `001-per-server-ide-integration`
**Created**: 2026-04-04
**Status**: Draft
**Input**: PHASE19.PRD — Per-Server Endpoints, Realtime Capability Refresh & IDE Integration

## User Scenarios & Testing *(mandatory)*

### User Story 1 - IDE Client Connects to Individual Backend Server (Priority: P1)

A developer using an IDE MCP client (VS Code Copilot, Cursor, Claude Code,
Windsurf) wants to configure their IDE to use a specific backend MCP server
through the gateway. Today, these clients expect individual server URLs in
their configuration files (`mcp.json`, settings). The developer copies a
per-server URL from the gateway's discovery document, pastes it into their
IDE configuration, and immediately has a working MCP connection to that
specific backend — with the full gateway middleware chain (auth, rate
limiting, policy) applied transparently.

**Why this priority**: This is the core use case driving Phase 19. Without
per-server endpoints, IDE clients cannot use the gateway at all. Every
other feature builds on this foundation.

**Independent Test**: Register a backend server, configure an IDE client
with the per-server URL, perform `tools/list` and `tools/call` operations,
and verify the client receives correct responses identical to a direct
backend connection.

**Acceptance Scenarios**:

1. **Given** a backend server "todoist" is registered and active, **When**
   a client sends `POST /mcp/servers/todoist/` with a valid `tools/list`
   request, **Then** the gateway returns the tool catalog from the todoist
   backend only.

2. **Given** a backend server "todoist" is registered and active, **When**
   a client sends a `tools/call` request via `/mcp/servers/todoist/`,
   **Then** the request is proxied to the todoist backend and the response
   is returned unmodified.

3. **Given** no backend server named "unknown" exists, **When** a client
   sends any request to `/mcp/servers/unknown/`, **Then** the gateway
   returns a 404 error.

4. **Given** a backend server "github" exists but its status is not
   "active", **When** a client sends a request to `/mcp/servers/github/`,
   **Then** the gateway returns a 503 error indicating the server is
   unavailable.

5. **Given** per-server endpoints are enabled, **When** an unauthenticated
   client sends a request to any per-server endpoint, **Then** the gateway
   returns 401, not revealing whether the server name exists.

---

### User Story 2 - Auto-Discovery of Available Servers (Priority: P1)

A developer or automated tool wants to discover which backend MCP servers
are available through the gateway without prior knowledge. The gateway
provides a standard discovery document at `/.well-known/mcp.json` that
lists all active servers with their URLs, names, versions, and status. A
separate listing endpoint provides programmatic enumeration with additional
metadata (tool count, resource count).

**Why this priority**: Discovery is essential for IDE clients that support
auto-configuration and for platform tooling that needs to enumerate
available servers. Without discovery, users must manually obtain URLs.

**Independent Test**: Query `/.well-known/mcp.json` and `GET /mcp/servers`
after registering multiple backends, verify the response lists all active
servers with correct metadata.

**Acceptance Scenarios**:

1. **Given** three backend servers are registered and active, **When** an
   authenticated client requests `GET /.well-known/mcp.json`, **Then** the
   response contains all three servers with their per-server URLs, names,
   versions, and active status.

2. **Given** one server is active and one is stale, **When** a client
   requests the discovery document, **Then** only the active server
   appears.

3. **Given** the discovery document was fetched 3 seconds ago, **When**
   another request arrives within the cache TTL, **Then** the gateway
   serves the cached response without regenerating.

4. **Given** a server registers or deregisters, **When** the next
   discovery request arrives, **Then** the response reflects the change
   (cache invalidated by the event).

5. **Given** an unauthenticated client, **When** it requests
   `/.well-known/mcp.json` or `GET /mcp/servers`, **Then** the gateway
   returns 401.

---

### User Story 3 - Realtime Tool Catalog Updates via Heartbeat (Priority: P2)

A backend server adds, removes, or modifies tools after its initial
registration. The gateway detects the change during the next heartbeat
cycle by comparing a capabilities hash and automatically refreshes its
internal tool index and search indexes. Search queries and `tools/list`
responses immediately reflect the updated catalog without requiring a
gateway restart or manual intervention.

**Why this priority**: Stale search indexes are the second problem
identified in the PRD. Without realtime refresh, progressive discovery
returns outdated results and tool catalog changes require gateway restarts.

**Independent Test**: Register a backend, change its tool catalog, send a
heartbeat with a new capabilities hash, then verify that search and
`tools/list` return the updated tools.

**Acceptance Scenarios**:

1. **Given** a backend "todoist" is active with 8 tools and hash "abc123",
   **When** the backend sends a heartbeat with hash "def456", **Then** the
   gateway calls `tools/list` on the backend, updates its internal index,
   and triggers an incremental search reindex.

2. **Given** a backend sends a heartbeat with the same hash as stored,
   **When** the gateway processes the heartbeat, **Then** no refresh is
   triggered (normal heartbeat behavior).

3. **Given** the gateway runs multiple pods, **When** one pod detects a
   capability change, **Then** it broadcasts an event so other pods
   refresh their indexes from the same backend.

4. **Given** a backend that does not send a capabilities hash (third-party
   server), **When** it sends a heartbeat, **Then** the gateway falls back
   to current behavior (no refresh triggered).

---

### User Story 4 - Push-Based Capability Refresh (Priority: P2)

A fleet backend server dynamically adds or removes tools and wants the
gateway to update immediately without waiting for the next heartbeat
interval. The server calls a push endpoint on the gateway, which triggers
an immediate tool catalog refresh and search reindex.

**Why this priority**: Push refresh provides a low-latency complement to
heartbeat-based detection. For fleet servers under the organization's
control, this eliminates the heartbeat interval delay entirely.

**Independent Test**: Register a backend, call the push refresh endpoint,
verify the gateway fetches the updated catalog and the search index
reflects changes within seconds.

**Acceptance Scenarios**:

1. **Given** a registered backend "todoist" with valid mTLS identity,
   **When** it calls the push refresh endpoint, **Then** the gateway
   fetches the current tool catalog, updates the capability index, and
   triggers incremental search reindex.

2. **Given** a caller whose identity does not match the registered
   server's identity, **When** it calls the push endpoint for that
   server, **Then** the gateway returns 403.

3. **Given** a backend calls the push endpoint more than 10 times in one
   minute, **When** the 11th call arrives, **Then** the gateway returns
   429 with a Retry-After header.

4. **Given** a push refresh completes, **When** the gateway has multiple
   pods, **Then** a capabilities-changed event is broadcast to all pods.

---

### User Story 5 - Server-Scoped API Key Access Control (Priority: P3)

An administrator creates an API key restricted to specific backend servers.
When a client authenticates with this scoped key, they can only access
tools on the allowed servers. Requests targeting other servers are denied.
Existing unscoped keys retain full gateway-wide access (backward
compatible).

**Why this priority**: As more teams and IDE users access the gateway,
fine-grained access control becomes necessary. This is an authorization
enhancement that builds on the per-server endpoint foundation.

**Independent Test**: Create a scoped API key, verify access to allowed
servers succeeds, verify access to disallowed servers returns 403.

**Acceptance Scenarios**:

1. **Given** an API key scoped to servers ["todoist", "github"], **When**
   the client calls a tool on "todoist", **Then** the request succeeds.

2. **Given** an API key scoped to servers ["todoist", "github"], **When**
   the client calls a tool on "slack", **Then** the gateway returns 403
   with a denial reason.

3. **Given** an API key with an empty server scope, **When** the client
   calls any server, **Then** the request succeeds (gateway-wide access,
   backward compatible).

4. **Given** a scoped key used on the unified `/mcp/` endpoint, **When**
   the tool resolves to a server outside the key's scope, **Then** the
   gateway returns 403 after tool-to-server resolution.

5. **Given** an administrator using the CLI, **When** they create a key
   with `--server-scope "todoist,github"`, **Then** the key is stored
   with the specified scope and enforced on subsequent requests.

---

### User Story 6 - Session-Affinity Routing for Stateful Backends (Priority: P3)

A backend server maintains session state (conversation context, open file
handles). When the backend has multiple replicas registered, the gateway
routes all requests from the same MCP session to the same replica, ensuring
stateful workflows are not broken by round-robin distribution.

**Why this priority**: Session affinity solves a correctness problem for
stateful backends but only affects servers with multiple replicas and
session state. Most backends are stateless, making this lower priority.

**Independent Test**: Register two replicas of the same server with
session-affinity routing enabled, send multiple requests with the same
session ID, verify all hit the same replica.

**Acceptance Scenarios**:

1. **Given** two replicas of server "assistant" with session affinity
   enabled, **When** a client sends 100 requests with the same MCP
   session ID, **Then** all 100 requests route to the same replica.

2. **Given** session affinity is enabled, **When** a client sends a
   request without a session ID header, **Then** the gateway falls back
   to round-robin routing.

3. **Given** two replicas and a session pinned to replica A, **When**
   replica A is removed, **Then** only sessions previously pinned to
   replica A are redistributed; sessions on replica B are unaffected.

4. **Given** a server with `routing_strategy` set to `round_robin`
   (default), **When** requests arrive with session IDs, **Then** the
   gateway ignores session affinity and uses round-robin.

---

### User Story 7 - Tool Namespace Isolation for Name Collisions (Priority: P3)

Multiple backend servers register tools with the same name (e.g., both
"todoist" and "github" offer a tool named `search`). The gateway resolves
the collision by namespacing tool names on the unified endpoint (e.g.,
`todoist.search`, `github.search`). Per-server endpoints are unaffected
since scope is implicit.

**Why this priority**: Tool name collisions are an operational scaling
issue. The current behavior (reject on conflict) is functional but does
not scale. This feature enables large fleet deployments.

**Independent Test**: Register two backends with a conflicting tool name,
verify `tools/list` on the unified endpoint returns namespaced names,
verify `tools/call` with a namespaced name routes correctly.

**Acceptance Scenarios**:

1. **Given** namespace mode is "namespace_on_conflict" and two servers
   both have a tool named "search", **When** a client calls `tools/list`
   on the unified endpoint, **Then** the response includes
   `todoist.search` and `github.search`.

2. **Given** namespace mode is "namespace_always", **When** a client calls
   `tools/list`, **Then** all tools are prefixed with their server name
   regardless of conflicts.

3. **Given** a namespaced tool name "todoist.search", **When** a client
   calls `tools/call` with that name on the unified endpoint, **Then**
   the gateway routes to the todoist backend's `search` tool.

4. **Given** a bare tool name "search" that is ambiguous (multiple
   servers), **When** a client calls `tools/call` with the bare name,
   **Then** the gateway returns an error listing available namespaced
   alternatives.

5. **Given** a per-server endpoint `/mcp/servers/todoist/`, **When** a
   client calls `tools/list`, **Then** tool names are bare (no namespace
   prefix), since scope is implicit.

---

### Edge Cases

- What happens when a server name contains special characters or exceeds
  the maximum length? The gateway validates against a strict pattern
  (lowercase alphanumeric, hyphens, underscores, max 63 characters) and
  rejects invalid names.
- What happens when the capabilities hash comparison triggers a
  `tools/list` call but the backend is temporarily unreachable? The
  gateway retains the existing tool catalog and retries on the next
  heartbeat.
- What happens when a server-scoped key is used on the unified endpoint
  for a tool hosted by a disallowed server? Denial occurs after
  tool-to-server resolution, returning 403 with a denial reason.
- What happens when a backend's tool catalog changes between cache TTL
  windows for the discovery document? The cache is invalidated by server
  registration/deregistration events. Mid-TTL tool changes are visible
  after the cache expires (max 5 seconds).
- What happens when a session-affinity hash collision occurs? The hashing
  algorithm distributes uniformly; collisions affect routing fairness but
  not correctness.
- What happens when the namespace separator character appears in a tool
  name? The gateway splits on the first separator only, treating the
  remainder as the tool name.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST expose each registered active backend server as
  an individual MCP-compliant endpoint at a deterministic URL path.
- **FR-002**: System MUST support the complete MCP protocol (`tools/list`,
  `tools/call`, `resources/list`, `resources/read`, `prompts/list`,
  `prompts/get`, SSE streaming) on each per-server endpoint.
- **FR-003**: System MUST run every per-server endpoint request through
  the full middleware chain (auth, rate limiting, policy enforcement).
- **FR-004**: System MUST provide a discovery document at
  `/.well-known/mcp.json` listing all active per-server endpoints with
  URL, name, version, and status.
- **FR-005**: System MUST provide a server listing endpoint returning
  JSON with server metadata including tool and resource counts.
- **FR-006**: System MUST cache the discovery document with a short TTL
  and invalidate on server registration or deregistration events.
- **FR-007**: System MUST compare a capabilities hash on each heartbeat
  and trigger a tool catalog refresh when a mismatch is detected.
- **FR-008**: System MUST support a push endpoint for immediate capability
  refresh, authenticated by the caller's identity matching the registered
  server.
- **FR-009**: System MUST incrementally update search indexes (full-text
  and vector) when a server's tool catalog changes, without rebuilding
  the entire index.
- **FR-010**: System MUST cache vector embeddings keyed by content hash,
  reusing cached embeddings for unchanged tools during incremental
  updates.
- **FR-011**: System MUST broadcast capability change events across
  gateway pods so all instances update their indexes.
- **FR-012**: System MUST support optional server-scope restrictions on
  API keys, limiting access to specified backend servers.
- **FR-013**: System MUST enforce server scope after authentication and
  after tool-to-server resolution on the unified endpoint.
- **FR-014**: System MUST provide a session-affinity routing strategy
  using consistent hashing on the MCP session ID header.
- **FR-015**: System MUST fall back to round-robin routing when no
  session ID is present or when session affinity is not configured for
  the target server.
- **FR-016**: System MUST support configurable tool namespace modes:
  reject conflicts, always namespace, or namespace only on conflict.
- **FR-017**: System MUST resolve namespaced tool names on the unified
  endpoint by splitting on the separator and filtering by server.
- **FR-018**: System MUST return bare (un-namespaced) tool names on
  per-server endpoints, since scope is implicit.
- **FR-019**: System MUST maintain full backward compatibility with the
  existing unified `/mcp/` endpoint behavior.
- **FR-020**: System MUST validate server names against a strict pattern
  and return consistent error responses to prevent name enumeration.
- **FR-021**: System MUST rate-limit the push refresh endpoint to prevent
  abuse.
- **FR-022**: System MUST persist a capabilities hash per server for
  change detection across gateway restarts.
- **FR-023**: System MUST persist server-scope restrictions on API keys
  with empty scope meaning gateway-wide access.
- **FR-024**: System MUST persist per-server routing strategy
  configuration with round-robin as the default.
- **FR-025**: System MUST expose configuration for all new features via
  environment variables with sensible defaults.

### Key Entities

- **Virtual Endpoint**: A per-server MCP-compliant URL that proxies to a
  single backend through the gateway. Derived from the server's registered
  name. Only routable when server status is "active".
- **Capabilities Hash**: A deterministic hash of a server's serialized
  tool and resource catalog, used for change detection during heartbeats.
  Stored per server. Compared on each heartbeat to trigger refresh.
- **Server Scope**: An optional restriction on API keys limiting access
  to a named set of backend servers. Empty scope means unrestricted.
  Enforced post-authentication.
- **Namespace Resolver**: A component that applies or strips server-name
  prefixes on tool names based on the configured namespace mode. Active
  on the unified endpoint only.
- **Session Affinity Key**: The MCP session ID header value used as input
  to consistent hashing for replica selection. Opaque to the gateway.
- **Embedding Cache**: A cache of vector embeddings keyed by content hash
  (tool name + description), enabling incremental reindexing without
  recomputing embeddings for unchanged tools.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: IDE clients (at least 3 distinct clients: VS Code Copilot,
  Cursor, Claude Code) can connect to any backend server through the
  gateway using only a URL change in their configuration.
- **SC-002**: Search results reflect tool catalog changes within one
  heartbeat interval (default 30 seconds) after a backend modifies its
  tools.
- **SC-003**: Push-based refresh updates search results within 5 seconds
  of the push call.
- **SC-004**: Embedding cache achieves greater than 90% hit rate for
  stable tool catalogs (tools unchanged between refreshes).
- **SC-005**: Zero breaking changes to the existing unified endpoint —
  the full existing test suite passes unchanged.
- **SC-006**: Server-scoped API keys block 100% of unauthorized
  cross-server access attempts.
- **SC-007**: Session-affinity routing delivers deterministic backend
  selection — 1000 requests with the same session ID all route to the
  same replica.
- **SC-008**: All new code meets or exceeds 80% test coverage per
  package.
- **SC-009**: Discovery document responses are served in under 5
  milliseconds under cache hit conditions.
- **SC-010**: Incremental reindex of a single tool on a 100-tool server
  recomputes only the changed tool's embedding, not the other 99.

## Assumptions

- IDE MCP clients support configuring individual server URLs and perform
  standard `tools/list` per server. No client-side gateway-specific
  modifications are expected.
- The existing middleware chain (auth, rate limiting, policy) is stable
  and requires no modifications to support per-server endpoints — only
  new route mounting.
- Backend servers that do not send a capabilities hash in heartbeats
  (third-party servers) continue to work with existing behavior; no
  refresh is triggered.
- The MCP Streamable HTTP transport's `Mcp-Session-Id` header is the
  standard mechanism for session correlation and is supported by clients
  that need session affinity.
- The push refresh endpoint is primarily for fleet servers under
  organizational control; third-party servers use heartbeat-based
  detection.
- The existing event bus (NATS/Dragonfly) is sufficient for broadcasting
  capability-change events across pods. No new messaging infrastructure
  is needed.
- Database migrations are additive (new columns with defaults) and
  require zero downtime. No backfill is necessary.
- The existing `CapabilityIndex` in-memory structure can be extended to
  support partitioned search indexing without architectural changes.
