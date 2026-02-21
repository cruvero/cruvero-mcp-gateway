# Phase 11: Distributed Rate Limiting & Multi-Gateway Communication

## Goal

Enable the gateway to scale beyond a single replica by implementing distributed rate limiting via Redis/DragonflyDB and cross-pod state synchronization for registrations, heartbeats, and classification changes.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [11A](PHASE11A.md) | Distributed Rate Limiting with Redis/DragonflyDB | [3 prompts](PHASE11A-PROMPT.md) | ratelimit, config |
| [11B](PHASE11B.md) | Multi-Gateway Registration Broadcast & Cross-Pod Sync | [3 prompts](PHASE11B-PROMPT.md) | registration, events, config |

## Dependencies

- Phase 10 (tool classification store, audit wiring, P0 fixes)
- Phase 5A (existing rate limiter store and middleware)
- Phase 3B (capability index, heartbeat processing)
- Phase 6A (NATS client, event publishing — for Cruvero mode sync)

## Deliverables

- `LimiterBackend` interface with three implementations: memory, Redis, NATS KV
- Sliding window counter via atomic Lua script in Redis/DragonflyDB
- Automatic fallback from Redis to in-memory on connection failure
- NATS (Cruvero mode) or Redis pub/sub (standalone mode) broadcast for registration events
- Any gateway pod can accept registrations and heartbeats — shared processing model
- Capability index refreshed on all pods within seconds of a registration change
- Tool classification cache invalidation broadcast when admin changes a classification
- Ingress resource with hash-based session affinity (optional, for connection reuse)

## Packages Modified

- `internal/ratelimit` — extract backend interface, add Redis and NATS backends
- `internal/registration` — add broadcast publisher and subscriber for registration events
- `internal/config` — add Redis URL, rate limit backend, pub/sub config
- `internal/events` — add registration broadcast subjects (for NATS mode)
- `cmd/mcpgw` — wire backend selection and subscribers

## New Dependencies (go.mod)

- `github.com/redis/go-redis/v9` — Redis client (works with DragonflyDB)
- `github.com/alicebob/miniredis/v2` — in-memory Redis for tests

## Success Criteria

- With 3 replicas and Redis backend, a client hitting different pods is rate-limited correctly across the aggregate
- Rate limit headers (`X-RateLimit-Limit`, `X-RateLimit-Remaining`, `Retry-After`) are accurate to within ±1 request
- If Redis goes down, gateway falls back to in-memory rate limiting with a warning log (no 500s)
- MCP server registers with Pod 1 → Pod 2 and Pod 3 see the new tools within 5 seconds
- Heartbeat processed by any pod updates the shared state
- Admin changes tool classification → all pods invalidate their cache within 5 seconds
- `go test ./...` passes with >=80% coverage on modified packages
