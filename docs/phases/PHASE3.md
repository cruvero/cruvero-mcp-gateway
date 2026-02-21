# Phase 3: Registration Protocol

## Goal

Implement the auto-registration protocol where MCP server pods register with the gateway via mTLS handshake. The gateway validates identity, stores registrations, tracks health via heartbeats, and maintains a capability index for routing.

## Sub-phases

| Sub-phase | Title | Prompts | Packages |
|-----------|-------|---------|----------|
| [3A](PHASE3A.md) | Registration Endpoint, Handshake, Approval | [3 prompts](PHASE3A-PROMPT.md) | registration |
| [3B](PHASE3B.md) | Heartbeat, State Machine, Capability Index | [3 prompts](PHASE3B-PROMPT.md) | registration |

## Dependencies

- Phase 1 (Core Foundation) -- types, config, server, store
- Phase 2 (Identity & Auth) -- mTLS middleware, SPIFFE ID extraction

## Deliverables

- POST /v1/registrations -- MCP server registration endpoint
- GET /v1/registrations -- list registered servers (admin)
- DELETE /v1/registrations/{id} -- deregister server
- POST /v1/registrations/{id}/heartbeat -- keepalive
- Server state machine: pending -> approved -> active -> stale -> expired
- Background sweeper for stale/expired registrations
- In-memory capability index: tool_name -> []ServerRecord for fast routing
- Re-indexing on registration lifecycle changes

## Packages Created

- `internal/registration` -- Registration handlers, state machine, heartbeat, capability index

## Success Criteria

- Full registration lifecycle works end-to-end with mTLS
- State machine transitions are correct and tested
- Capability index is accurate and updates on changes
- Stale/expired registrations are cleaned up automatically
- >=80% test coverage
