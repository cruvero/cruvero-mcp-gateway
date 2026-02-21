# Phase 6: NATS & Cruvero Integration

## Goal

Implement event-driven synchronization with Cruvero via NATS. The gateway publishes registration lifecycle events and subscribes to configuration updates from Cruvero. When integrated, Cruvero is the source of truth for policy and server configuration.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [6A](PHASE6A.md) | NATS Events, Publishing, Subscribing | [3 prompts](PHASE6A-PROMPT.md) | events |
| [6B](PHASE6B.md) | Config Sync from Cruvero, Graceful Degradation | [3 prompts](PHASE6B-PROMPT.md) | events |

## Dependencies

- Phase 3 (Registration Protocol) — lifecycle events to publish
- Phase 4 (MCP Proxy & Routing) — routing state affected by config sync
- Phase 5 (Policy & Rate Limiting) — policy profiles updated via config sync

## Deliverables

- NATS client with connection management and reconnection handling
- Event publishing on registration lifecycle changes
- Event types: server.registered, server.deregistered, server.health_changed, policy.violated
- Subject naming: mcpgw.{gateway_id}.events.{type} (publish), mcpgw.{gateway_id}.config.> (subscribe)
- Config subscription: policy, servers, auth config from Cruvero
- Graceful degradation: gateway continues with last-known config if NATS unavailable
- Full config snapshot request on NATS reconnection
- Health endpoint reflects NATS connectivity status

## Packages Created

- `internal/events` — NATS client, event types, publishing, subscribing, config sync

## Success Criteria

- Events published on all registration lifecycle changes
- Config updates from Cruvero correctly applied to gateway state
- Gateway continues operating when NATS is disconnected
- Reconnection triggers full config sync
- Health endpoint shows NATS status
- >=80% test coverage
