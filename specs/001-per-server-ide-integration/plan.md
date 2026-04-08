# Implementation Plan: Per-Server Endpoints, Realtime Capability Refresh & IDE Integration

**Branch**: `001-per-server-ide-integration` | **Date**: 2026-04-04 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-per-server-ide-integration/spec.md`

## Summary

Enable IDE MCP clients to connect to individual backend servers through the
gateway via per-server virtual endpoints (`/mcp/servers/{name}/`), with auto-
discovery via `/.well-known/mcp.json`. Eliminate search index staleness by
detecting tool catalog changes during heartbeats (capabilities hash comparison)
and via a push refresh endpoint, triggering incremental BM25 and vector
reindexing. Add server-scoped API keys for fine-grained authorization,
session-affinity routing via consistent hashing for stateful backends, and
tool namespace isolation to resolve name collisions at scale.

## Technical Context

**Language/Version**: Go 1.25.7
**Primary Dependencies**: mcp-go v0.45.0, chi v5.2.5, lib/pq, nats.go v1.49.0, go-redis v9.18.0, otel v1.42.0
**Storage**: PostgreSQL (required), NATS JetStream KV (optional), DragonflyDB (optional)
**Testing**: Go standard `testing` package, table-driven tests, `go test ./...`
**Target Platform**: Linux (Kubernetes), single static binary, distroless container
**Project Type**: Web service (mTLS reverse proxy / MCP gateway)
**Performance Goals**: Sub-5ms discovery doc cache hit, incremental reindex touches only changed tools, >90% embedding cache hit rate
**Constraints**: Zero downtime migrations, full backward compatibility with unified `/mcp/` endpoint, 80% min test coverage per package
**Scale/Scope**: Multi-pod Kubernetes deployment, 10+ backend servers, 100+ tools across fleet

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Status | Evidence |
|---|-----------|--------|----------|
| I | Full Middleware Chain — No Bypass | PASS | Per-server endpoints mount inside auth+ratelimit+policy middleware group in `server.go`. Discovery and listing endpoints require authentication. |
| II | MCP Protocol Fidelity | PASS | VirtualServerHandler passes full MCP protocol through to single backend via existing BackendClient. No protocol modification. |
| III | Security by Default | PASS | Server-scoped keys default to enforcement ON. Push endpoint requires mTLS identity match. Per-server endpoints return 401 to unauthenticated, 404 without leaking server existence. |
| IV | Resilience and Graceful Degradation | PASS | Heartbeat hash mismatch retries on next heartbeat if backend unreachable. Embedding cache falls back gracefully. Session affinity falls back to round-robin. |
| V | Backward Compatibility | PASS | Unified `/mcp/` endpoint unchanged. All migrations additive (new columns with defaults). Existing test suite untouched. |
| VI | Observability | PASS | 8 new Prometheus metrics, 4 new trace spans, 3 new audit event types defined in PRD. |
| VII | Test-Driven Quality Gates | PASS | Unit tests per file (≥80%), integration tests for cross-component flows, table-driven style. |

## Project Structure

### Documentation (this feature)

```text
specs/001-per-server-ide-integration/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── virtual-server-endpoints.md
│   ├── capability-refresh-endpoints.md
│   └── server-scope-api.md
└── tasks.md             # Phase 2 output (NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/
├── proxy/
│   ├── virtual_server.go          # NEW: VirtualServerHandler, server resolution
│   ├── virtual_server_test.go     # NEW: Handler tests
│   ├── discovery_doc.go           # NEW: .well-known/mcp.json generation + cache
│   ├── discovery_doc_test.go      # NEW: Discovery document tests
│   ├── server_list.go             # NEW: GET /mcp/servers listing
│   ├── server_list_test.go        # NEW: Server list tests
│   ├── session_affinity.go        # NEW: SessionAffinityStrategy + rendezvous hash
│   ├── session_affinity_test.go   # NEW: Distribution + replica add/remove tests
│   ├── namespace.go               # NEW: NamespaceResolver for tool name conflicts
│   ├── namespace_test.go          # NEW: Resolution tests for all modes
│   ├── router.go                  # MODIFY: Strategy selection based on server config
│   ├── server.go                  # MODIFY: Wire virtual server handler
│   ├── tools.go                   # MODIFY: Apply namespace in tools/list aggregation
│   └── cache.go                   # MODIFY: Add InvalidateByServer (already exists)
├── registration/
│   ├── refresh.go                 # NEW: RefreshCapabilities logic, hash comparison
│   ├── refresh_test.go            # NEW: Refresh logic tests
│   ├── refresh_handler.go         # NEW: HTTP handler for POST capabilities push
│   ├── refresh_handler_test.go    # NEW: Handler tests
│   ├── heartbeat.go               # MODIFY: Trigger refresh on hash mismatch
│   └── index.go                   # MODIFY: Track conflict state per tool name
├── search/
│   ├── bm25.go                    # MODIFY: Partitioned index with lazy IDF
│   ├── bm25_test.go               # MODIFY: Partitioned index tests
│   ├── vector.go                  # MODIFY: Incremental update by server partition
│   ├── vector_test.go             # MODIFY: Incremental update tests
│   ├── embedding_cache.go         # NEW: LRU embedding cache keyed by content hash
│   ├── embedding_cache_test.go    # NEW: Cache tests
│   └── hybrid.go                  # MODIFY: Wire incremental update through
├── auth/
│   ├── server_scope.go            # NEW: ServerScopeMiddleware
│   └── server_scope_test.go       # NEW: Scope enforcement tests
├── events/
│   └── types.go                   # MODIFY: Add ServerCapabilitiesChanged event
├── types/
│   └── types.go                   # MODIFY: Add ServerScope, RoutingStrategy fields
├── config/
│   └── config.go                  # MODIFY: Add 10 new MCPGW_* env vars
├── store/
│   └── interfaces.go              # MODIFY: Add ServerScope to APIKey types
├── server/
│   └── server.go                  # MODIFY: Mount new routes, insert middleware
├── admin/
│   └── apikeys.go                 # MODIFY: Display/edit server scope
└── cmd/mcpgw/
    └── apikey.go                  # MODIFY: Add --server-scope flag

migrations/
├── 0015_server_capabilities_hash.up.sql    # NEW
├── 0015_server_capabilities_hash.down.sql  # NEW
├── 0016_apikey_server_scope.up.sql         # NEW
├── 0016_apikey_server_scope.down.sql       # NEW
├── 0017_server_routing_strategy.up.sql     # NEW
└── 0017_server_routing_strategy.down.sql   # NEW

deploy/helm/mcpgw/
└── values.yaml                    # MODIFY: Add feature flag env var mappings
```

**Structure Decision**: This feature extends the existing Go service structure.
All new code lives within existing `internal/` packages following established
patterns. No new top-level directories or packages needed. 17 new files,
16 modified files, 6 new migration files.

## Complexity Tracking

> No constitution violations. All changes follow existing patterns.

| Aspect | Complexity | Justification |
|--------|-----------|---------------|
| Partitioned BM25 index | Medium | Required for incremental reindex without full rebuild. Existing flat index cannot update a single server's tools. |
| Embedding cache | Medium | Required to avoid O(N) embedding recomputation on single-tool changes. Content-hash keying is standard. |
| Rendezvous hashing | Low | Stateless consistent hashing. No external state, no session tables. Standard algorithm. |
| Namespace resolver | Medium | Three modes with different behaviors. Integrates into existing tools/list and tools/call paths. |
