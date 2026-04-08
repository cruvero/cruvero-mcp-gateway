# Tasks: Per-Server Endpoints, Realtime Capability Refresh & IDE Integration

**Input**: Design documents from `/specs/001-per-server-ide-integration/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US7)
- Exact file paths included in every task

---

## Phase 1: Setup

**Purpose**: Configuration, type extensions, and database migrations shared across all user stories.

- [x] T001 Add 10 new MCPGW_* environment variables to `internal/config/config.go`: `PER_SERVER_ENDPOINTS`, `WELL_KNOWN_ENABLED`, `WELL_KNOWN_CACHE_TTL`, `CAPABILITY_REFRESH_ENABLED`, `CAPABILITY_PUSH_ENABLED`, `EMBEDDING_CACHE_MAX_SIZE`, `SERVER_SCOPE_ENFORCEMENT`, `DEFAULT_ROUTING_STRATEGY`, `TOOL_NAMESPACE_MODE`, `NAMESPACE_SEPARATOR` with defaults per data-model.md
- [x] T002 [P] Add `RoutingStrategy string` field to `ServerRecord` in `internal/types/types.go`; add `ServerScope []string` to API key types in `internal/types/types.go` or `internal/store/` types
- [x] T003 [P] Add `EventServerCapabilitiesChanged = "server.capabilities_changed"` constant and `ServerCapabilitiesChangedPayload` struct to `internal/events/types.go`
- [x] T004 [P] Create migration `migrations/0015_server_capabilities_hash.up.sql` and `.down.sql` — `ALTER TABLE mcp_servers ADD COLUMN IF NOT EXISTS capabilities_hash TEXT NOT NULL DEFAULT ''`
- [x] T005 [P] Create migration `migrations/0016_apikey_server_scope.up.sql` and `.down.sql` — `ALTER TABLE api_keys ADD COLUMN server_scope TEXT[] NOT NULL DEFAULT '{}'`
- [x] T006 [P] Create migration `migrations/0017_server_routing_strategy.up.sql` and `.down.sql` — `ALTER TABLE mcp_servers ADD COLUMN routing_strategy TEXT NOT NULL DEFAULT 'round_robin'`
- [x] T007 Extend `ServerStore` interface in `internal/store/interfaces.go` to read/write `routing_strategy` column; extend `APIKeyStore` interface to read/write `server_scope` column; update PostgreSQL store implementations accordingly

**Checkpoint**: All shared types, config, migrations, and store interfaces ready. User story implementation can begin.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure changes that multiple user stories depend on. MUST complete before user story phases.

- [x] T008 Extend `RoutingStrategy` interface in `internal/proxy/router.go`: change `Select(candidates []types.ServerRecord) *types.ServerRecord` to `Select(ctx context.Context, candidates []types.ServerRecord, req *RoutingRequest) (*types.ServerRecord, error)` with new `RoutingRequest` struct containing `SessionID` and `ToolName` fields
- [x] T009 Update `RoundRobinStrategy.Select` in `internal/proxy/router.go` to match new signature (ignore `req`, return `nil` error); update all call sites in `Router.Route()`
- [x] T010 Implement `RefreshCapabilities` function in `internal/registration/refresh.go`: accept server record, call `tools/list` via BackendClient, update CapabilityIndex, trigger search reindex, store new hash, broadcast change event, invalidate ToolCache. This is the shared refresh logic used by both heartbeat (US3) and push (US4)
- [x] T011 [P] Write tests for `RefreshCapabilities` in `internal/registration/refresh_test.go`: hash comparison, index update, event broadcast, backend-unreachable fallback

**Checkpoint**: Routing interface updated, refresh logic ready. All user stories can now proceed.

---

## Phase 3: User Story 1 — IDE Client Connects to Individual Backend Server (P1)

**Goal**: Expose each active backend as a fully MCP-compliant endpoint at `/mcp/servers/{name}/`.

**Independent Test**: Register a backend, send `tools/list` and `tools/call` to its per-server URL, verify correct single-server response.

### Implementation

- [x] T012 [US1] Create `VirtualServerHandler` in `internal/proxy/virtual_server.go`: extract `{serverName}` from chi URL param, validate against `^[a-z0-9][a-z0-9_-]{0,62}$`, resolve server via CapabilityIndex, validate status == active (404 if not found, 503 if not active), lazily create per-server MCP server instance (cached in `sync.Map`), forward MCP request to that instance's StreamableHTTP handler
- [x] T013 [US1] Implement per-server MCP server instance lifecycle in `internal/proxy/virtual_server.go`: `getOrCreateServerInstance(serverName)` creates `mcp-go` MCPServer + StreamableHTTPServer scoped to a single backend's tools/resources/prompts. Cache invalidation on capability refresh or deregistration events
- [x] T014 [US1] Mount per-server routes in `internal/server/server.go`: `r.Route("/mcp/servers/{serverName}", ...)` with full middleware chain (auth, ratelimit, policy). Include `/sse` sub-route for SSE transport variant. Gate behind `cfg.PerServerEndpoints`
- [x] T015 [US1] Write tests in `internal/proxy/virtual_server_test.go`: table-driven tests for server resolution (active → 200, not found → 404, inactive → 503, unauthenticated → 401), server name validation (valid, invalid chars, too long), MCP passthrough (tools/list returns single server's tools, tools/call proxied correctly), instance cache invalidation

**Checkpoint**: Per-server endpoints functional. IDE clients can connect via `/mcp/servers/{name}/`.

---

## Phase 4: User Story 2 — Auto-Discovery of Available Servers (P1)

**Goal**: Provide `/.well-known/mcp.json` and `GET /mcp/servers` for programmatic server discovery.

**Independent Test**: Register multiple backends, query discovery endpoints, verify correct server list with metadata.

### Implementation

- [x] T016 [P] [US2] Create discovery document handler in `internal/proxy/discovery_doc.go`: generate `/.well-known/mcp.json` by iterating active servers in CapabilityIndex. Implement TTL-based cache using `atomic.Pointer[cachedDiscoveryDoc]`. Accept gateway base URL from config or `X-Forwarded-Host` header. Gate behind `cfg.WellKnownEnabled`
- [x] T017 [P] [US2] Create server listing handler in `internal/proxy/server_list.go`: `GET /mcp/servers` returns JSON array with name, url, version, status, tool_count, resource_count. Respects server scope (filtered by authenticated identity's scope)
- [x] T018 [US2] Wire discovery cache invalidation: subscribe to `server.registered` and `server.deregistered` events via existing Broadcaster in `internal/proxy/discovery_doc.go`. On event, set cache to nil (forces regeneration on next request)
- [x] T019 [US2] Mount discovery and listing routes in `internal/server/server.go`: `/.well-known/mcp.json` and `GET /mcp/servers` inside auth middleware group
- [x] T020 [P] [US2] Write tests in `internal/proxy/discovery_doc_test.go`: JSON structure correctness, only active servers included, cache hit (same response within TTL), cache invalidation on registration event, unauthenticated → 401
- [x] T021 [P] [US2] Write tests in `internal/proxy/server_list_test.go`: correct metadata (tool/resource counts), scope-filtered results, empty list when no active servers, unauthenticated → 401

**Checkpoint**: Discovery endpoints operational. IDE clients can auto-discover servers.

---

## Phase 5: User Story 3 — Realtime Tool Catalog Updates via Heartbeat (P2)

**Goal**: Detect tool catalog changes during heartbeats and trigger incremental search reindex.

**Independent Test**: Register a backend, change its tools, send heartbeat with new hash, verify search returns updated tools.

### Implementation

- [x] T022 [US3] Modify `Service.Heartbeat()` in `internal/registration/heartbeat.go`: when `req.CapabilityHash` is non-empty and differs from `server.CapabilityHash`, call `RefreshCapabilities()` (from T010). On successful refresh, update stored hash. On backend-unreachable, log warning and continue normal heartbeat. Echo `capabilities_hash` in heartbeat response
- [x] T023 [US3] Implement partitioned BM25 index in `internal/search/bm25.go`: refactor `BM25Engine` to store `partitions map[string]*bm25Partition` keyed by serverID. Add `AddPartition(serverID, docs)`, `RemovePartition(serverID)`, `UpdatePartition(serverID, docs)`. Maintain `globalIDF` with `idfDirty` flag. Lazy IDF recomputation on first query after partition change. Preserve existing `Index()` and `Search()` methods for backward compatibility
- [x] T024 [P] [US3] Create LRU embedding cache in `internal/search/embedding_cache.go`: `EmbeddingCache` struct with `Get(key)`, `Set(key, embedding)`, `ContentHash(name, description)` methods. LRU eviction when `maxSize` exceeded. Thread-safe via `sync.RWMutex`
- [x] T025 [US3] Add server-partitioned storage to `VectorEngine` in `internal/search/vector.go`: partition embeddings by serverID parallel to BM25. On incremental update: compute content hashes, check `EmbeddingCache`, embed only new/changed tools, evict removed tools from cache
- [x] T026 [US3] Wire incremental update through `HybridEngine` in `internal/search/hybrid.go`: add `UpdatePartition(serverID, docs)` and `RemovePartition(serverID)` that delegate to both BM25 and Vector engines
- [x] T027 [P] [US3] Write tests for partitioned BM25 in `internal/search/bm25_test.go`: add/remove/update partitions, lazy IDF recomputation correctness, query returns results across partitions, backward compatibility (full Index() still works)
- [x] T028 [P] [US3] Write tests for embedding cache in `internal/search/embedding_cache_test.go`: cache hit/miss, LRU eviction order, content hash determinism, concurrent access, max size enforcement
- [x] T029 [P] [US3] Write tests for incremental vector update in `internal/search/vector_test.go`: incremental update re-embeds only changed tools, cache reuse for unchanged tools, partition removal clears embeddings

**Checkpoint**: Heartbeat-based refresh operational. Search indexes update incrementally on tool changes.

---

## Phase 6: User Story 4 — Push-Based Capability Refresh (P2)

**Goal**: Fleet servers can push capability changes to the gateway without waiting for heartbeat.

**Independent Test**: Register a backend, call push endpoint, verify gateway fetches updated catalog within seconds.

### Implementation

- [x] T030 [US4] Create push refresh HTTP handler in `internal/registration/refresh_handler.go`: `POST /v1/registrations/{id}/capabilities`. Validate mTLS identity matches registered server's SPIFFE ID. Rate limit 10 req/min per server. Call `RefreshCapabilities()` (from T010). Return `{tools_count, resources_count, reindex_triggered, capabilities_hash}`
- [x] T031 [US4] Add `PublishServerCapabilitiesChanged()` method to publisher in `internal/events/publisher.go`: publish `server.capabilities_changed` event with `{server_id, capabilities_hash, tool_names[]}` payload via NATS
- [x] T032 [US4] Subscribe to `server.capabilities_changed` events for cross-pod sync: in `internal/registration/broadcast.go` or gateway startup, on receiving event, trigger `RefreshCapabilities()` on the affected backend to update local CapabilityIndex and search indexes
- [x] T033 [US4] Mount push endpoint route in `internal/server/server.go`: `POST /v1/registrations/{id}/capabilities` inside mTLS-authenticated registration route group. Gate behind `cfg.CapabilityPushEnabled`
- [x] T034 [P] [US4] Write tests in `internal/registration/refresh_handler_test.go`: identity validation (match → 200, mismatch → 403), rate limiting (11th call → 429), correct response payload, event broadcast triggered on success

**Checkpoint**: Push refresh operational. Fleet servers get immediate index updates.

---

## Phase 7: User Story 5 — Server-Scoped API Key Access Control (P3)

**Goal**: API keys can be restricted to specific backend servers.

**Independent Test**: Create scoped key, verify access to allowed server succeeds, verify access to disallowed server returns 403.

### Implementation

- [x] T035 [US5] Create `ServerScopeMiddleware` in `internal/auth/server_scope.go`: extract authenticated identity's `ServerScope` from context. For per-server endpoints, extract `{serverName}` from URL and check against scope. For unified endpoint, set scope in context for deferred check in proxy handler. Empty scope = pass (gateway-wide). Denied → 403 with `X-Denied-Reason: server_scope` and JSON body per contract
- [x] T036 [US5] Add post-resolution scope check in `internal/proxy/server.go` or `internal/proxy/router.go`: after `CapabilityIndex.FindServers(toolName)` resolves target server on unified `/mcp/` endpoint, check server name against identity's `ServerScope` from context. Denied → 403
- [x] T037 [US5] Insert `ServerScopeMiddleware` into middleware chain in `internal/server/server.go`: after auth, before policy. Gate behind `cfg.ServerScopeEnforcement`
- [x] T038 [P] [US5] Add `--server-scope` flag to `mcpgw apikey create` in `cmd/mcpgw/apikey.go`: comma-separated server names, passed to store on key creation
- [x] T039 [P] [US5] Add `server_scope` column display and edit to admin dashboard API keys page in `internal/admin/apikeys.go`: render as tag list, support HTMX form editing
- [x] T040 [P] [US5] Write tests in `internal/auth/server_scope_test.go`: empty scope = pass, matching scope = pass, non-matching scope = 403, per-server endpoint enforcement, unified endpoint deferred check, scope check runs after auth (unauthenticated → 401 not 403)
- [x] T041 [US5] Write audit logging for scope denials: log `server_scope.denied` events with `client_id`, `server_name`, `requested_tool` via existing audit store. Increment `gateway_server_scope_denied_total` Prometheus counter

**Checkpoint**: Server-scoped keys enforce per-server authorization.

---

## Phase 8: User Story 6 — Session-Affinity Routing for Stateful Backends (P3)

**Goal**: Requests with the same MCP session ID consistently route to the same backend replica.

**Independent Test**: Register two replicas, send 100 requests with same session ID, verify all route to same replica.

### Implementation

- [x] T042 [US6] Create `SessionAffinityStrategy` in `internal/proxy/session_affinity.go`: implement `RoutingStrategy` interface. Extract session ID from `RoutingRequest.SessionID`. If empty, delegate to fallback `RoundRobinStrategy`. Use rendezvous (HRW) hashing with FNV-1a 64-bit: for each candidate compute `hash(sessionID + candidateID)`, select highest score
- [x] T043 [US6] Modify strategy selection in `internal/proxy/router.go`: resolve server's `routing_strategy` field (from ServerRecord or config default). If `session_affinity`, use `SessionAffinityStrategy`; otherwise `RoundRobinStrategy`. Extract `Mcp-Session-Id` header from request context and pass in `RoutingRequest.SessionID`
- [x] T044 [P] [US6] Write tests in `internal/proxy/session_affinity_test.go`: deterministic routing (same session ID → same replica across 1000 calls), fallback to round-robin when no session ID, minimal redistribution on replica removal (only affected sessions move), O(n) candidate evaluation, uniform distribution across replicas for different session IDs

**Checkpoint**: Session affinity routing operational for stateful backends.

---

## Phase 9: User Story 7 — Tool Namespace Isolation for Name Collisions (P3)

**Goal**: Tool name collisions across backends resolved via configurable namespacing on unified endpoint.

**Independent Test**: Register two backends with conflicting tool name, verify namespaced names in tools/list, verify namespaced tools/call routes correctly.

### Implementation

- [x] T045 [US7] Create `NamespaceResolver` in `internal/proxy/namespace.go`: implement three modes (`reject`, `namespace_always`, `namespace_on_conflict`). `ApplyNamespace(toolName, serverName)` returns namespaced name based on mode and conflict state. `ResolveNamespace(namespacedName)` splits on first separator to extract server hint and bare tool name. Detect conflicts via CapabilityIndex (tool with len(servers) > 1)
- [x] T046 [US7] Integrate NamespaceResolver into `tools/list` aggregation in `internal/proxy/tools.go`: in `handleListTools()` / `toolAggregator`, apply namespace to federated tool names based on configured mode. Per-server endpoints skip namespacing (bare names always)
- [x] T047 [US7] Integrate NamespaceResolver into `tools/call` resolution in `internal/proxy/router.go`: in `resolveFederatedName()`, if name contains separator, extract server hint and filter by server. If bare name is ambiguous, return error listing available namespaced alternatives
- [x] T048 [P] [US7] Write tests in `internal/proxy/namespace_test.go`: all three modes (reject, namespace_always, namespace_on_conflict), conflict detection, namespaced `tools/call` resolution, bare-name ambiguity error with alternatives, per-server endpoint returns bare names, separator handling (split on first only)

**Checkpoint**: Namespace isolation operational. Tool name collisions resolved at scale.

---

## Phase 10: Polish & Cross-Cutting Concerns

**Purpose**: Observability instrumentation, Helm chart updates, and quality gate verification across all stories.

- [x] T049 Add Prometheus metrics for all new features: `gateway_virtual_server_requests_total`, `gateway_capability_refresh_total`, `gateway_capability_refresh_duration_seconds`, `gateway_embedding_cache_hits_total`, `gateway_embedding_cache_misses_total`, `gateway_server_scope_denied_total`, `gateway_session_affinity_hits_total`, `gateway_tool_namespace_conflicts_total` — registered in appropriate handler/middleware files
- [x] T050 [P] Add OpenTelemetry trace spans: `mcpgw.virtual_server.handle`, `mcpgw.capability.refresh`, `mcpgw.search.incremental_reindex`, `mcpgw.routing.session_affinity` — in VirtualServerHandler, RefreshCapabilities, search engines, and SessionAffinityStrategy respectively
- [x] T051 [P] Update Helm chart `deploy/helm/mcpgw/values.yaml`: add all 10 new feature flag env var mappings under appropriate sections (perServerEndpoints, capabilityRefresh, serverScopeEnforcement, routing, toolNamespace)
- [x] T052 Run full test suite (`go test ./...`) and verify all existing tests pass unchanged. Run `go vet ./...` and `golangci-lint run ./...`. Verify ≥80% coverage per new package with `go test -coverprofile`
- [x] T053 Run `govulncheck ./...` to verify no known vulnerabilities introduced by changes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2
- **US2 (Phase 4)**: Depends on Phase 2 — can run in parallel with US1
- **US3 (Phase 5)**: Depends on Phase 2 — can run in parallel with US1/US2
- **US4 (Phase 6)**: Depends on Phase 2 + T010 (RefreshCapabilities from Phase 2)
- **US5 (Phase 7)**: Depends on Phase 2 — can run in parallel with US1–US4
- **US6 (Phase 8)**: Depends on T008/T009 (RoutingStrategy interface from Phase 2)
- **US7 (Phase 9)**: Depends on Phase 2 — can run in parallel with US1–US6
- **Polish (Phase 10)**: Depends on all user stories being complete

### User Story Dependencies

- **US1 (P1)**: Foundational only — no dependencies on other stories
- **US2 (P1)**: Foundational only — no dependencies on other stories (can parallel with US1)
- **US3 (P2)**: Foundational only — uses RefreshCapabilities (T010)
- **US4 (P2)**: Foundational only — uses RefreshCapabilities (T010), can parallel with US3
- **US5 (P3)**: Foundational only — independently testable
- **US6 (P3)**: Foundational (T008/T009) — independently testable
- **US7 (P3)**: Foundational only — independently testable

### Within Each User Story

- Implementation tasks in dependency order (models → services → handlers → route mounting)
- Tests can run in parallel with each other [P]
- Story complete before moving to next priority

### Parallel Opportunities

- T002, T003, T004, T005, T006 can all run in parallel (different files)
- T016, T017 can run in parallel (different files within US2)
- T020, T021 can run in parallel (different test files within US2)
- T024, T027, T028, T029 can run in parallel with other US3 tasks (different files)
- T034, T038, T039, T040 can run in parallel (different files)
- US1, US2, US3, US4, US5, US6, US7 can all run in parallel after Phase 2

---

## Parallel Example: Setup Phase

```bash
# All type/config/migration tasks in parallel:
T002: Add RoutingStrategy + ServerScope to types.go
T003: Add event type to events/types.go
T004: Create migration 0015
T005: Create migration 0016
T006: Create migration 0017
```

## Parallel Example: User Stories after Foundational

```bash
# All P1 stories in parallel:
US1: VirtualServerHandler (T012-T015)
US2: Discovery + Listing (T016-T021)

# All P2 stories in parallel:
US3: Heartbeat refresh + incremental reindex (T022-T029)
US4: Push refresh + event broadcast (T030-T034)

# All P3 stories in parallel:
US5: Server-scoped keys (T035-T041)
US6: Session affinity (T042-T044)
US7: Namespace isolation (T045-T048)
```

---

## Implementation Strategy

### MVP First (US1 + US2 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: US1 — Per-Server Endpoints
4. Complete Phase 4: US2 — Auto-Discovery
5. **STOP and VALIDATE**: IDE clients can connect and discover servers
6. Deploy/demo if ready

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 + US2 → IDE clients operational (MVP!)
3. US3 + US4 → Realtime search freshness
4. US5 → Fine-grained authorization
5. US6 → Stateful backend support
6. US7 → Scale-ready namespace isolation
7. Polish → Full observability + quality gates

### Parallel Team Strategy

With multiple developers after Foundational:

- Developer A: US1 (Per-Server Endpoints) + US2 (Discovery)
- Developer B: US3 (Heartbeat Refresh) + US4 (Push Refresh)
- Developer C: US5 (Scoped Keys) + US6 (Session Affinity) + US7 (Namespacing)

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story is independently completable and testable
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Tests included per the project's 80% coverage requirement
