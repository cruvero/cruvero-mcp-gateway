# Phase 11B: Multi-Gateway Registration Broadcast & Cross-Pod Sync

## Overview

Implement cross-pod synchronization so that registrations, heartbeats, and tool classification changes on one gateway pod are visible to all other pods within seconds. Use NATS pub/sub in Cruvero mode and Redis pub/sub in standalone mode.

## Scope

### Broadcast Interface (`internal/registration/broadcast.go`)

Define a broadcast abstraction that works over NATS or Redis:

```go
type Broadcaster interface {
    // Publish sends a message to all subscribers on the given subject.
    Publish(ctx context.Context, subject string, data []byte) error
    
    // Subscribe registers a handler for messages on the given subject.
    Subscribe(subject string, handler func(data []byte)) error
    
    // Close unsubscribes and releases resources.
    Close() error
}
```

Subjects:
- `mcpgw.registry.updated` — a server was registered, deregistered, or changed status
- `mcpgw.registry.heartbeat` — a heartbeat was processed
- `mcpgw.classification.updated` — a tool classification was changed

### NATS Broadcaster (`internal/registration/nats_broadcast.go`)

For Cruvero-integrated mode (NATS already deployed):

- Wraps the existing `events.Client` NATS connection.
- `Publish()`: delegates to `nats.Conn.Publish()`.
- `Subscribe()`: delegates to `nats.Conn.Subscribe()` with a message handler.
- `Close()`: unsubscribes all active subscriptions.

### Redis Broadcaster (`internal/registration/redis_broadcast.go`)

For standalone mode (Redis already deployed for rate limiting):

- Uses `redis.Client.Publish()` and `redis.Client.Subscribe()`.
- Subscriber runs in a goroutine processing the `*redis.PubSub` channel.
- `Close()`: closes the pubsub and stops the goroutine.

### Registration Event Messages

```go
type RegistrationEvent struct {
    EventType string `json:"event_type"` // "registered", "deregistered", "status_changed"
    ServerID  string `json:"server_id"`
    Timestamp int64  `json:"timestamp"`
}

type ClassificationEvent struct {
    ToolName  string `json:"tool_name"`
    RiskLevel string `json:"risk_level"`
    UpdatedBy string `json:"updated_by"`
    Timestamp int64  `json:"timestamp"`
}
```

### Registration Broadcast Publisher (`internal/registration/service.go`)

Modify the registration service to publish events after state changes:

- After `Register()` succeeds: publish `RegistrationEvent{EventType: "registered", ServerID: id}`.
- After `Deregister()` succeeds: publish `RegistrationEvent{EventType: "deregistered", ServerID: id}`.
- After heartbeat changes server status: publish `RegistrationEvent{EventType: "status_changed", ServerID: id}`.

### Registration Broadcast Subscriber (`internal/registration/subscriber.go`)

New component that subscribes to registration events and refreshes local state:

```go
type RegistrationSubscriber struct {
    broadcaster Broadcaster
    index       *CapabilityIndex
    store       store.ServerStore
    logger      *slog.Logger
}

func NewRegistrationSubscriber(b Broadcaster, index *CapabilityIndex, store store.ServerStore, logger *slog.Logger) *RegistrationSubscriber

func (s *RegistrationSubscriber) Start(ctx context.Context) error {
    return s.broadcaster.Subscribe("mcpgw.registry.updated", func(data []byte) {
        var event RegistrationEvent
        if err := json.Unmarshal(data, &event); err != nil {
            s.logger.Error("failed to unmarshal registration event", "error", err)
            return
        }
        s.handleEvent(ctx, event)
    })
}

func (s *RegistrationSubscriber) handleEvent(ctx context.Context, event RegistrationEvent) {
    switch event.EventType {
    case "registered", "status_changed":
        // Refresh this specific server from DB
        server, err := s.store.Get(ctx, event.ServerID)
        if err != nil {
            s.logger.Error("failed to refresh server", "server_id", event.ServerID, "error", err)
            return
        }
        if server.IsRoutable() {
            s.index.Add(*server)
        } else {
            s.index.Remove(event.ServerID)
        }
    case "deregistered":
        s.index.Remove(event.ServerID)
    }
    s.logger.Info("processed registration broadcast",
        "event_type", event.EventType,
        "server_id", event.ServerID,
    )
}
```

### Classification Broadcast Subscriber

Subscribe to `mcpgw.classification.updated` and invalidate the classification cache:

```go
func (s *ClassificationSubscriber) Start(ctx context.Context) error {
    return s.broadcaster.Subscribe("mcpgw.classification.updated", func(data []byte) {
        var event ClassificationEvent
        if err := json.Unmarshal(data, &event); err != nil {
            return
        }
        s.classificationCache.Invalidate(event.ToolName)
        s.logger.Info("invalidated classification cache",
            "tool_name", event.ToolName,
            "new_risk_level", event.RiskLevel,
        )
    })
}
```

### Capability Index Enhancement (`internal/registration/index.go`)

- Add `RefreshServer(ctx context.Context, serverID string, store store.ServerStore) error`:
  - Fetch the server from DB.
  - If routable: add/update in index. If not routable: remove.
  - This is a targeted refresh, not a full rebuild.
- Existing `Rebuild()` remains for startup initialization.

### Shared Heartbeat Processing

Any pod can process any heartbeat. The flow:

1. Pod receives heartbeat for server X.
2. Updates DB (`last_heartbeat`, status transition if needed).
3. Publishes `mcpgw.registry.heartbeat` with server ID.
4. All pods (including the one that processed it) refresh server X in their local index.

This eliminates the need for sticky sessions.

### Ingress Resource (`charts/mcpgateway/templates/ingress.yaml`)

Add an Ingress resource for external IDE access:

```yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "mcpgateway.fullname" . }}
  annotations:
    {{- toYaml .Values.ingress.annotations | nindent 4 }}
spec:
  ingressClassName: {{ .Values.ingress.className }}
  tls:
    - hosts:
        - {{ .Values.ingress.host }}
      secretName: {{ .Values.ingress.tlsSecretName }}
  rules:
    - host: {{ .Values.ingress.host }}
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: {{ include "mcpgateway.fullname" . }}
                port:
                  number: {{ .Values.service.port }}
{{- end }}
```

With values:
```yaml
ingress:
  enabled: false
  className: nginx
  host: gateway.corp.example.com
  tlsSecretName: gateway-tls
  annotations: {}
```

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/registration/broadcast.go` | Broadcaster interface definition |
| `internal/registration/nats_broadcast.go` | NATS pub/sub implementation |
| `internal/registration/redis_broadcast.go` | Redis pub/sub implementation |
| `internal/registration/broadcast_test.go` | Tests for both implementations |
| `internal/registration/subscriber.go` | Registration event subscriber |
| `internal/registration/subscriber_test.go` | Tests |
| `internal/registration/classification_subscriber.go` | Classification cache invalidation subscriber |
| `internal/registration/service.go` | Add broadcast publishing after state changes |
| `internal/registration/index.go` | Add RefreshServer method |
| `cmd/mcpgw/serve.go` | Wire broadcaster and subscribers |
| `charts/mcpgateway/templates/ingress.yaml` | New Ingress resource |
| `charts/mcpgateway/values.yaml` | Add ingress config block |

## Testing Requirements

- Broadcaster: test publish/subscribe round-trip for both NATS and Redis implementations
- Registration subscriber: test event handling — registered event adds to index, deregistered removes, status_changed updates
- Classification subscriber: test cache invalidation on event
- Index RefreshServer: test add, update, remove paths
- Heartbeat broadcast: test that heartbeat on Pod A triggers index refresh on Pod B (simulated with two subscriber instances)
- Integration: test end-to-end — register server → broadcast → subscriber refreshes index → tools/call routes correctly
- Helm: `helm lint` and `helm template` pass with ingress enabled/disabled
- Coverage: >=80%
