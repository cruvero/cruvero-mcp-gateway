# Phase 6A: NATS Events, Publishing, Subscribing

## Overview

Implement the NATS client infrastructure, event type definitions, and the publish/subscribe framework for gateway events.

## Scope

### NATS Client (`internal/events/client.go`)
- `Client` struct: conn *nats.Conn, gatewayID string, logger *slog.Logger, connected atomic.Bool
- `NewClient(url string, gatewayID string, opts ...ClientOption) (*Client, error)`
- ClientOption pattern for: TLS config, reconnect wait, max reconnects, name
- Connection lifecycle:
  - Connect with reconnect handlers
  - On disconnect: log warning, set connected=false
  - On reconnect: log info, set connected=true, trigger re-subscribe
  - On close: clean shutdown
- `Publish(subject string, data []byte) error`
- `Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error)`
- `IsConnected() bool`
- `Close() error`

### Event Types (`internal/events/types.go`)
- `EventEnvelope` struct: EventType string, Timestamp time.Time, GatewayID string, Payload json.RawMessage (JSON tags)
- Event type constants:
  - EventServerRegistered = "server.registered"
  - EventServerDeregistered = "server.deregistered"
  - EventServerHealthChanged = "server.health_changed"
  - EventPolicyViolated = "policy.violated"
- Payload types (each with JSON tags):
  - `ServerRegisteredPayload`: ServerID, Name, SPIFFEid, Capabilities, Endpoint
  - `ServerDeregisteredPayload`: ServerID, Name, Reason
  - `ServerHealthChangedPayload`: ServerID, Name, OldStatus, NewStatus
  - `PolicyViolatedPayload`: ClientID, ToolName, Violations []string, Decision string
- Subject naming:
  - Publish: `mcpgw.{gateway_id}.events.{event_type}` (e.g., `mcpgw.gw-1.events.server.registered`)
  - Subscribe config: `mcpgw.{gateway_id}.config.>` (wildcard for all gateway-scoped config subjects)

### Event Publisher (`internal/events/publisher.go`)
- `Publisher` struct: client *Client, logger *slog.Logger
- `NewPublisher(client *Client, logger *slog.Logger) *Publisher`
- `PublishServerRegistered(ctx context.Context, server types.ServerRecord) error`
- `PublishServerDeregistered(ctx context.Context, serverID, name, reason string) error`
- `PublishServerHealthChanged(ctx context.Context, serverID, name string, oldStatus, newStatus types.ServerStatus) error`
- `PublishPolicyViolated(ctx context.Context, clientID, toolName string, violations []string, decision string) error`
- Each method: build payload, wrap in EventEnvelope, marshal to JSON, publish to correct subject
- If client not connected: log warning, return nil (don't block on NATS unavailability)

### Integration with Registration
- Hook publisher into registration Service:
  - After successful registration -> PublishServerRegistered
  - After deregistration -> PublishServerDeregistered
  - After state machine transition -> PublishServerHealthChanged
- Hook publisher into policy Engine:
  - After policy violation -> PublishPolicyViolated

## Files Created

| File | Description |
|------|-------------|
| internal/events/client.go | NATS client with reconnection |
| internal/events/types.go | Event envelope and payload types |
| internal/events/publisher.go | Event publishing methods |
| internal/events/client_test.go | Client connection tests |
| internal/events/types_test.go | Serialization tests |
| internal/events/publisher_test.go | Publisher tests |

## Testing Requirements

- Client: test connection, test reconnection callback, test IsConnected state tracking
- Types: test JSON serialization/deserialization of all event types
- Publisher: mock client, verify correct subjects and payloads, test graceful handling when disconnected
- Use nats-server test helper or mock for unit tests
- Coverage: >=80%
