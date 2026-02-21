# Phase 6A Implementation Prompts

## Prompt 1 of 3: NATS Client & Event Types

### Required Reading (read these files before writing code)
- docs/phases/PHASE6A.md
- internal/config/config.go (MCPGW_NATS_URL, MCPGW_GATEWAY_ID)
- internal/types/types.go

### Task

Create the NATS client and event type definitions.

1. `internal/events/types.go`:
   - Define all event type constants (EventServerRegistered, etc.)
   - Define EventEnvelope struct with JSON tags
   - Define all payload structs with JSON tags
   - Define subject helper: SubjectForEvent(gatewayID, eventType string) string -> "mcpgw." + gatewayID + ".events." + eventType

2. `internal/events/client.go`:
   - Define ClientOption functional options: WithTLS(*tls.Config), WithReconnectWait(time.Duration), WithMaxReconnects(int)
   - Define Client struct
   - NewClient(url, gatewayID string, opts ...ClientOption) (*Client, error):
     - Apply options
     - Set up nats.Options with disconnect/reconnect/close handlers
     - Connect to NATS
   - Publish, Subscribe, IsConnected, Close methods
   - Disconnect handler: log + set connected=false
   - Reconnect handler: log + set connected=true

3. Tests:
   - `internal/events/types_test.go`: Test JSON round-trip for EventEnvelope and each payload type, test SubjectForEvent
   - `internal/events/client_test.go`: Test with embedded nats-server (or mock interface for unit tests)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All event types properly defined with JSON tags
- Client handles connection lifecycle
- Subject naming follows convention

---

## Prompt 2 of 3: Event Publisher

### Required Reading (read these files before writing code)
- internal/events/client.go
- internal/events/types.go
- internal/types/types.go (ServerRecord, ServerStatus)

### Task

Create the event publisher.

1. `internal/events/publisher.go`:
   - Define Publisher struct: client *Client, gatewayID string, logger *slog.Logger
   - NewPublisher(client *Client, gatewayID string, logger *slog.Logger) *Publisher
   - Helper method: publish(ctx context.Context, eventType string, payload any) error
     - Marshal payload to JSON
     - Create EventEnvelope with eventType, time.Now(), gatewayID, payload
     - Marshal envelope
     - Call client.Publish with correct subject
     - If client not connected: log at warn level, return nil
   - PublishServerRegistered: build ServerRegisteredPayload from ServerRecord, call publish
   - PublishServerDeregistered: build payload, call publish
   - PublishServerHealthChanged: build payload, call publish
   - PublishPolicyViolated: build payload, call publish

2. `internal/events/publisher_test.go`:
   - Define mock Client interface for testing (or use nats test server)
   - Test each Publish method produces correct subject and payload
   - Test publish when disconnected doesn't error
   - Test envelope structure (timestamp, gateway_id present)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All lifecycle events published correctly
- Graceful handling when NATS disconnected
- Correct subject naming for each event type

---

## Prompt 3 of 3: Registration & Policy Integration

### Required Reading (read these files before writing code)
- internal/events/publisher.go
- internal/registration/service.go
- internal/policy/engine.go

### Task

Wire the publisher into registration and policy components.

1. Update registration Service:
   - Add optional publisher field (nil if NATS not configured)
   - After Register: call publisher.PublishServerRegistered if publisher != nil
   - After Deregister: call publisher.PublishServerDeregistered
   - In sweeper state transitions: call publisher.PublishServerHealthChanged

2. Update policy Engine:
   - Add optional publisher field
   - After Evaluate with violations: call publisher.PublishPolicyViolated

3. Update server wiring:
   - If MCPGW_NATS_URL configured: create NATS client, create publisher, inject into services
   - If not configured: services work without publisher (standalone mode)

4. Tests:
   - Test registration publishes events (mock publisher)
   - Test registration works without publisher (standalone)
   - Test policy violations trigger event publishing

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Events published on all lifecycle changes
- Standalone mode works without NATS
- No panics when publisher is nil
- >=80% coverage
