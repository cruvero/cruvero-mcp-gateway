# Phase 3B Implementation Prompts

## Prompt 1 of 3: State Machine & Heartbeat

### Required Reading (read these files before writing code)
- docs/phases/PHASE3B.md
- internal/registration/service.go
- internal/registration/handler.go
- internal/types/types.go
- internal/store/interfaces.go

### Task

Create the state machine and heartbeat handler.

1. `internal/registration/state.go`:
   - Define Event string type with constants: EventHeartbeat, EventHeartbeatMissed, EventExpired, EventApproved, EventDeregistered
   - Implement Transition(current ServerStatus, event Event) (ServerStatus, error):
     - Build transition map of valid (current, event) -> next combinations
     - Return error for invalid transitions with descriptive message
   - Write `internal/registration/state_test.go`:
     - Table-driven tests for all valid transitions
     - Test invalid transitions return error

2. `internal/registration/heartbeat.go`:
   - Define HeartbeatRequest struct: Status string (optional), Metadata map[string]string (optional)
   - Define HeartbeatResponse struct: ServerStatus, NextDeadline time.Time
   - Add Heartbeat method to Service: validate identity ownership, update heartbeat, apply state transition
   - Add handleHeartbeat to Handler: POST /{id}/heartbeat route
   - Add route to Routes() method

3. `internal/registration/heartbeat_test.go`:
   - Test heartbeat with valid identity -> 200
   - Test heartbeat with wrong SPIFFE ID -> 403
   - Test heartbeat for unknown registration -> 404
   - Test state transition from approved -> active on first heartbeat

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All state transitions correctly implemented
- Heartbeat updates timestamp and triggers transitions
- Ownership validation works correctly

---

## Prompt 2 of 3: Capability Index

### Required Reading (read these files before writing code)
- docs/phases/PHASE3B.md
- internal/types/types.go
- internal/registration/state.go

### Task

Create the in-memory capability index.

1. `internal/registration/index.go`:
   - Define CapabilityIndex struct with:
     - mu sync.RWMutex
     - tools map[string][]types.ServerRecord (tool name -> servers)
     - resources map[string][]types.ServerRecord (resource prefix -> servers)
   - NewCapabilityIndex() *CapabilityIndex
   - Rebuild(servers []types.ServerRecord): clear and rebuild from scratch, only include routable servers (IsRoutable() == true)
   - Add(server types.ServerRecord): add capabilities to index maps
   - Remove(serverID string): remove server from all index entries
   - LookupTool(name string) []types.ServerRecord: return copy of matching servers
   - LookupResource(prefix string) []types.ServerRecord: match by longest prefix
   - ListTools() []string: return deduplicated sorted list
   - ListResources() []string: return deduplicated sorted list

2. `internal/registration/index_test.go`:
   - Test Add then LookupTool finds server
   - Test Add multiple servers for same tool
   - Test Remove clears server from all entries
   - Test Rebuild from list of servers
   - Test LookupTool returns empty for unknown tool
   - Test ListTools returns sorted deduplicated names
   - Test concurrent Add/Lookup doesn't race (use -race flag)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Thread-safe with RWMutex
- Correct lookup behavior
- Race-free under concurrent access
- Returns copies, not references to internal state

---

## Prompt 3 of 3: Background Sweeper

### Required Reading (read these files before writing code)
- docs/phases/PHASE3B.md
- internal/registration/index.go
- internal/registration/state.go
- internal/store/interfaces.go
- internal/config/config.go

### Task

Create the background sweeper that manages stale and expired registrations.

1. `internal/registration/sweeper.go`:
   - Define Sweeper struct: store ServerStore, index *CapabilityIndex, config *config.Config, logger *slog.Logger
   - NewSweeper constructor
   - Start(ctx context.Context): launch goroutine with ticker at HeartbeatTTL interval
   - sweep() method (called each tick):
     - Call store.ListStale(threshold=HeartbeatTTL) -> update each to status=stale, log
     - Call store.ListExpired(threshold=3*HeartbeatTTL) -> update each to status=expired, remove from index, log
     - Log summary: "sweep complete" with counts
   - Stop via context cancellation

2. `internal/registration/sweeper_test.go`:
   - Mock store returning stale servers -> verify UpdateStatus called with stale
   - Mock store returning expired servers -> verify UpdateStatus called with expired and index.Remove called
   - Test Start/Stop lifecycle (use short tick interval)
   - Test sweep with no stale/expired servers (no-op)
   - Test sweep error handling (store errors logged, not fatal)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Sweeper correctly identifies and transitions stale/expired servers
- Index updated on expiry
- Clean shutdown via context cancellation
- Errors logged but don't crash the sweeper
- >=80% coverage
