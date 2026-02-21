# Phase 3B: Heartbeat, State Machine, Capability Index

## Overview

Implement the heartbeat protocol, server state machine transitions, background cleanup of stale registrations, and the in-memory capability index used for tool routing.

## Scope

### Heartbeat Handler (`internal/registration/heartbeat.go`)

**POST /v1/registrations/{id}/heartbeat**
- Requires mTLS identity matching the registered server's SPIFFE ID
- Updates last_heartbeat timestamp in store
- Optionally accepts status update in body (e.g., healthy/degraded with metadata)
- Transitions server to active if currently approved
- Returns 200 with current server status and next heartbeat deadline
- Returns 404 if registration not found
- Returns 403 if SPIFFE ID doesn't match

### State Machine (`internal/registration/state.go`)
- Define valid transitions:
  - pending -> approved (on admin approval or auto-approve)
  - approved -> active (on first heartbeat)
  - active -> stale (on missed heartbeat, 1x TTL)
  - active -> active (on heartbeat received)
  - stale -> active (on heartbeat received)
  - stale -> expired (on missed heartbeat, 3x TTL)
  - expired -> (removed from routing, eligible for cleanup)
  - any -> (removed on explicit deregistration)
- `Transition(current ServerStatus, event Event) (ServerStatus, error)` -- validates transition
- Event types: EventHeartbeat, EventHeartbeatMissed, EventExpired, EventApproved, EventDeregistered

### Background Sweeper (`internal/registration/sweeper.go`)
- `Sweeper` struct: store, config, index (capability index), logger, ticker
- `Start(ctx context.Context)` -- runs on configurable interval (MCPGW_HEARTBEAT_TTL)
- Each tick:
  1. Query ListStale: servers where last_heartbeat > 1x TTL ago and status = active -> set status = stale
  2. Query ListExpired: servers where last_heartbeat > 3x TTL ago -> set status = expired, remove from capability index
  3. Log transitions
- `Stop()` -- clean shutdown

### Capability Index (`internal/registration/index.go`)
- `CapabilityIndex` struct with sync.RWMutex protection
- In-memory map: tool_name -> []ServerRecord (for tools/call routing)
- In-memory map: resource_prefix -> []ServerRecord (for resources/read routing)
- `Rebuild(servers []ServerRecord)` -- full rebuild from store data
- `Add(server ServerRecord)` -- add server's capabilities to index
- `Remove(serverID string)` -- remove server from all index entries
- `LookupTool(toolName string) []ServerRecord` -- find servers offering a tool
- `LookupResource(prefix string) []ServerRecord` -- find servers for a resource URI
- `ListTools() []string` -- all known tool names (deduplicated)
- Thread-safe: reads use RLock, writes use Lock

## Files Created

| File | Description |
|------|-------------|
| internal/registration/heartbeat.go | Heartbeat handler |
| internal/registration/state.go | State machine transitions |
| internal/registration/sweeper.go | Background stale/expired cleanup |
| internal/registration/index.go | In-memory capability index |
| internal/registration/heartbeat_test.go | Heartbeat tests |
| internal/registration/state_test.go | State machine tests |
| internal/registration/sweeper_test.go | Sweeper tests |
| internal/registration/index_test.go | Index tests |

## Testing Requirements

- Heartbeat: test valid heartbeat updates timestamp, test SPIFFE ID mismatch returns 403, test not found returns 404
- State machine: test all valid transitions succeed, test invalid transitions return error
- Sweeper: test stale detection, test expired detection, test index update on expiry
- Index: test Add/Remove/Lookup correctness, test thread safety with concurrent access, test Rebuild produces correct state
- Coverage: >=80%
