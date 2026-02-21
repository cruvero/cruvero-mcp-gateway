# Phase 11B Implementation Prompts

## Prompt 1 of 3: Broadcast Interface & Implementations

### Required Reading (read these files before writing code)
- docs/phases/PHASE11B.md (full scope, broadcast interface and implementations)
- internal/events/client.go (existing NATS client for reference)
- internal/ratelimit/redis_backend.go (existing Redis client usage from Phase 11A)
- LLM.md (conventions)

### Task

Create the broadcast abstraction and both NATS and Redis implementations.

1. `internal/registration/broadcast.go`:
   ```go
   package registration
   
   import "context"
   
   // Broadcaster abstracts cross-pod event distribution.
   // Implementations exist for NATS (Cruvero mode) and Redis (standalone mode).
   type Broadcaster interface {
       Publish(ctx context.Context, subject string, data []byte) error
       Subscribe(subject string, handler func(data []byte)) error
       Close() error
   }
   
   // Broadcast subjects.
   const (
       SubjectRegistryUpdated       = "mcpgw.registry.updated"
       SubjectClassificationUpdated = "mcpgw.classification.updated"
   )
   
   // RegistrationEvent is published when a server's registration state changes.
   type RegistrationEvent struct {
       EventType string `json:"event_type"` // "registered", "deregistered", "status_changed"
       ServerID  string `json:"server_id"`
       Timestamp int64  `json:"timestamp"`  // Unix milliseconds
   }
   
   // ClassificationEvent is published when a tool's risk classification changes.
   type ClassificationEvent struct {
       ToolName  string `json:"tool_name"`
       RiskLevel string `json:"risk_level"`
       UpdatedBy string `json:"updated_by"`
       Timestamp int64  `json:"timestamp"`
   }
   ```

2. `internal/registration/nats_broadcast.go`:
   - `NATSBroadcaster` struct wrapping `*nats.Conn` (get from existing `events.Client.Conn()` — you may need to add a `Conn()` accessor to `events.Client` if not already present).
   - `NewNATSBroadcaster(conn *nats.Conn, logger *slog.Logger) *NATSBroadcaster`
   - `Publish()`: `conn.Publish(subject, data)`.
   - `Subscribe()`: `conn.Subscribe(subject, func(msg *nats.Msg) { handler(msg.Data) })`. Store the `*nats.Subscription` for cleanup.
   - `Close()`: unsubscribe all stored subscriptions.

3. `internal/registration/redis_broadcast.go`:
   - `RedisBroadcaster` struct wrapping `*redis.Client`.
   - `NewRedisBroadcaster(client *redis.Client, logger *slog.Logger) *RedisBroadcaster`
   - `Publish()`: `client.Publish(ctx, subject, data)`.
   - `Subscribe()`:
     - Create `client.Subscribe(context.Background(), subject)`.
     - Start a goroutine that reads from the pubsub channel and calls `handler(msg.Payload)`.
     - Store the `*redis.PubSub` for cleanup.
   - `Close()`: close all pubsub connections, wait for goroutines to stop.
   - **Thread safety**: use a mutex to protect the subscriptions slice.

4. **No-op broadcaster** `internal/registration/noop_broadcast.go`:
   - `NoopBroadcaster` for single-replica mode (when neither NATS nor Redis is available).
   - `Publish()`: no-op, return nil.
   - `Subscribe()`: no-op, return nil.
   - `Close()`: no-op.

5. **Tests** `internal/registration/broadcast_test.go`:
   - NATS: use embedded NATS server. Test publish→subscribe round-trip. Test multiple subjects. Test Close unsubscribes.
   - Redis: use miniredis. Test publish→subscribe round-trip. Test multiple subjects. Test Close stops goroutines.
   - NoopBroadcaster: verify no panics on all methods.
   - Test with JSON marshaled `RegistrationEvent` and `ClassificationEvent`.

### Verification
```bash
go test ./internal/registration/...
go vet ./...
```

### Acceptance Criteria
- Both NATS and Redis broadcasters implement the Broadcaster interface
- Publish→subscribe works within the same process (simulating cross-pod)
- Multiple subscriptions on different subjects are independent
- Close() cleanly releases all resources
- NoopBroadcaster is safe for single-replica mode

---

## Prompt 2 of 3: Registration & Classification Subscribers

### Required Reading (read these files before writing code)
- docs/phases/PHASE11B.md (subscriber sections)
- internal/registration/broadcast.go (Broadcaster interface, event types)
- internal/registration/index.go (CapabilityIndex, existing methods)
- internal/store/interfaces.go (ServerStore, ToolClassificationStore)
- internal/policy/classification_cache.go (ClassificationCache from Phase 10B)

### Task

Create subscribers that react to broadcast events and update local state.

1. **Capability index enhancement** `internal/registration/index.go`:
   - Add `RefreshServer(ctx context.Context, serverID string, serverStore store.ServerStore) error`:
     ```go
     func (idx *CapabilityIndex) RefreshServer(ctx context.Context, serverID string, serverStore store.ServerStore) error {
         server, err := serverStore.Get(ctx, serverID)
         if err != nil {
             return fmt.Errorf("refresh server %s: %w", serverID, err)
         }
         
         idx.mu.Lock()
         defer idx.mu.Unlock()
         
         // Remove old entries for this server
         idx.removeServerLocked(serverID)
         
         // Re-add if routable
         if server != nil && server.IsRoutable() {
             idx.addServerLocked(*server)
         }
         
         return nil
     }
     ```
   - Extract `removeServerLocked` and `addServerLocked` private methods from existing `Remove` and `Add` (they already hold the lock externally).

2. **Registration subscriber** `internal/registration/subscriber.go`:
   ```go
   type RegistrationSubscriber struct {
       broadcaster Broadcaster
       index       *CapabilityIndex
       serverStore store.ServerStore
       logger      *slog.Logger
   }
   
   func NewRegistrationSubscriber(
       b Broadcaster,
       index *CapabilityIndex,
       serverStore store.ServerStore,
       logger *slog.Logger,
   ) *RegistrationSubscriber
   
   func (s *RegistrationSubscriber) Start(ctx context.Context) error {
       return s.broadcaster.Subscribe(SubjectRegistryUpdated, func(data []byte) {
           var event RegistrationEvent
           if err := json.Unmarshal(data, &event); err != nil {
               s.logger.Error("unmarshal registration event failed", "error", err)
               return
           }
           
           switch event.EventType {
           case "registered", "status_changed":
               if err := s.index.RefreshServer(ctx, event.ServerID, s.serverStore); err != nil {
                   s.logger.Error("failed to refresh server in index",
                       "server_id", event.ServerID,
                       "error", err,
                   )
               }
           case "deregistered":
               s.index.Remove(event.ServerID)
           default:
               s.logger.Warn("unknown registration event type", "type", event.EventType)
           }
           
           s.logger.Debug("processed registration broadcast",
               "event_type", event.EventType,
               "server_id", event.ServerID,
           )
       })
   }
   ```

3. **Classification subscriber** `internal/registration/classification_subscriber.go`:
   ```go
   type ClassificationSubscriber struct {
       broadcaster Broadcaster
       cache       *policy.ClassificationCache
       logger      *slog.Logger
   }
   
   func NewClassificationSubscriber(
       b Broadcaster,
       cache *policy.ClassificationCache,
       logger *slog.Logger,
   ) *ClassificationSubscriber
   
   func (s *ClassificationSubscriber) Start(ctx context.Context) error {
       return s.broadcaster.Subscribe(SubjectClassificationUpdated, func(data []byte) {
           var event ClassificationEvent
           if err := json.Unmarshal(data, &event); err != nil {
               s.logger.Error("unmarshal classification event failed", "error", err)
               return
           }
           
           s.cache.Invalidate(event.ToolName)
           s.logger.Info("invalidated classification cache via broadcast",
               "tool_name", event.ToolName,
               "new_risk_level", event.RiskLevel,
               "updated_by", event.UpdatedBy,
           )
       })
   }
   ```

4. **Registration service broadcast** `internal/registration/service.go`:
   - Add `broadcaster Broadcaster` field and `SetBroadcaster(b Broadcaster)` setter.
   - After `Register()` succeeds (after DB write):
     ```go
     s.publishEvent(ctx, "registered", serverID)
     ```
   - After `Deregister()` succeeds:
     ```go
     s.publishEvent(ctx, "deregistered", serverID)
     ```
   - After heartbeat status change:
     ```go
     s.publishEvent(ctx, "status_changed", serverID)
     ```
   - Helper:
     ```go
     func (s *Service) publishEvent(ctx context.Context, eventType, serverID string) {
         if s.broadcaster == nil {
             return
         }
         event := RegistrationEvent{
             EventType: eventType,
             ServerID:  serverID,
             Timestamp: time.Now().UnixMilli(),
         }
         data, err := json.Marshal(event)
         if err != nil {
             s.logger.Error("marshal registration event failed", "error", err)
             return
         }
         if err := s.broadcaster.Publish(ctx, SubjectRegistryUpdated, data); err != nil {
             s.logger.Error("broadcast registration event failed",
                 "event_type", eventType,
                 "server_id", serverID,
                 "error", err,
             )
         }
     }
     ```
   - **Important**: Broadcast errors are logged but NEVER fail the operation. The DB is the source of truth; broadcast is best-effort.

5. **Tests:**
   - `internal/registration/subscriber_test.go`:
     - Use NoopBroadcaster or a channel-based mock broadcaster for deterministic testing.
     - Test "registered" event → index contains the server.
     - Test "deregistered" event → index does not contain the server.
     - Test "status_changed" event → index updated.
     - Test malformed JSON → error logged, no panic.
   - `internal/registration/classification_subscriber_test.go`:
     - Test classification event → cache entry invalidated.
   - `internal/registration/service_test.go`:
     - Test that Register publishes "registered" event.
     - Test that Deregister publishes "deregistered" event.
     - Test that broadcast failure doesn't fail the operation.
     - Test with nil broadcaster (no panic).

### Verification
```bash
go test ./internal/registration/... ./internal/policy/...
go vet ./...
```

### Acceptance Criteria
- Registration on Pod A triggers index refresh on Pod B (via broadcast + subscriber)
- Deregistration removes server from all pods' indexes
- Classification change invalidates cache on all pods
- Broadcast errors are logged but never fail the primary operation
- Nil broadcaster is handled gracefully (no panic)
- RefreshServer does a targeted DB read (not full rebuild)

---

## Prompt 3 of 3: Wiring, Ingress, & Integration Test

### Required Reading (read these files before writing code)
- docs/phases/PHASE11B.md (wiring and ingress sections)
- cmd/mcpgw/serve.go (existing component wiring pattern)
- charts/mcpgateway/values.yaml (existing Helm values)
- charts/mcpgateway/templates/ (existing template patterns)

### Task

Wire all broadcast components together in serve.go, add the Ingress resource, and write an integration test.

1. **Wiring** in `cmd/mcpgw/serve.go`:
   ```go
   // Select broadcaster based on available infrastructure
   var broadcaster registration.Broadcaster
   switch {
   case cfg.CruveroEnabled && natsClient != nil && natsClient.IsConnected():
       broadcaster = registration.NewNATSBroadcaster(natsClient.Conn(), logger)
       logger.Info("using NATS broadcaster for cross-pod sync")
   case cfg.RateLimitBackend == "redis" && redisClient != nil:
       broadcaster = registration.NewRedisBroadcaster(redisClient, logger)
       logger.Info("using Redis broadcaster for cross-pod sync")
   default:
       broadcaster = registration.NewNoopBroadcaster()
       logger.Info("no broadcaster available, running single-replica mode")
   }
   defer broadcaster.Close()
   
   // Wire broadcaster into registration service
   registrationService.SetBroadcaster(broadcaster)
   
   // Start subscribers
   regSubscriber := registration.NewRegistrationSubscriber(
       broadcaster, capabilityIndex, serverStore, logger,
   )
   if err := regSubscriber.Start(ctx); err != nil {
       return fmt.Errorf("start registration subscriber: %w", err)
   }
   
   classSubscriber := registration.NewClassificationSubscriber(
       broadcaster, classificationCache, logger,
   )
   if err := classSubscriber.Start(ctx); err != nil {
       return fmt.Errorf("start classification subscriber: %w", err)
   }
   ```
   
   - Note: `redisClient` needs to be extracted as a shared variable accessible to both the rate limiter and the broadcaster. If the rate limiter creates its own client internally, either:
     - Extract the client creation to serve.go and pass it to both.
     - Or create a second client for the broadcaster (Redis handles multiple connections fine).

2. **Ingress resource** `charts/mcpgateway/templates/ingress.yaml`:
   ```yaml
   {{- if .Values.ingress.enabled }}
   apiVersion: networking.k8s.io/v1
   kind: Ingress
   metadata:
     name: {{ include "mcpgateway.fullname" . }}
     labels:
       {{- include "mcpgateway.labels" . | nindent 4 }}
     {{- with .Values.ingress.annotations }}
     annotations:
       {{- toYaml . | nindent 4 }}
     {{- end }}
   spec:
     {{- if .Values.ingress.className }}
     ingressClassName: {{ .Values.ingress.className }}
     {{- end }}
     {{- if .Values.ingress.tls }}
     tls:
       - hosts:
           - {{ .Values.ingress.host }}
         secretName: {{ .Values.ingress.tlsSecretName | default (printf "%s-tls" (include "mcpgateway.fullname" .)) }}
     {{- end }}
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

3. **Helm values** additions to `charts/mcpgateway/values.yaml`:
   ```yaml
   ingress:
     enabled: false
     className: ""
     host: gateway.example.com
     tls: true
     tlsSecretName: ""
     annotations: {}
       # nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
       # nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
       # nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
   ```

4. **Integration test** `internal/registration/integration_test.go`:
   Write a test that simulates two gateway pods with shared state:
   ```go
   func TestMultiPodRegistrationSync(t *testing.T) {
       // Use miniredis as shared broadcaster
       mr, _ := miniredis.Run()
       defer mr.Close()
       
       redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
       
       // Simulate two pods
       broadcaster1 := NewRedisBroadcaster(redisClient, slog.Default())
       broadcaster2 := NewRedisBroadcaster(redisClient, slog.Default())
       
       // Each pod has its own index
       index1 := NewCapabilityIndex()
       index2 := NewCapabilityIndex()
       
       // Shared DB (use mock)
       mockStore := testutil.NewMockServerStore()
       
       // Pod 2 subscribes
       sub := NewRegistrationSubscriber(broadcaster2, index2, mockStore, slog.Default())
       sub.Start(context.Background())
       
       // Pod 1 registers a server (this publishes an event)
       server := types.ServerRecord{
           ID: "server-1", Name: "test-server",
           Status: types.StatusActive,
           Capabilities: types.Capability{Tools: []string{"tool_a", "tool_b"}},
       }
       mockStore.Create(context.Background(), &server)
       index1.Add(server)
       
       // Publish the event (simulating what registration service does)
       event := RegistrationEvent{
           EventType: "registered",
           ServerID:  "server-1",
           Timestamp: time.Now().UnixMilli(),
       }
       data, _ := json.Marshal(event)
       broadcaster1.Publish(context.Background(), SubjectRegistryUpdated, data)
       
       // Wait for subscriber to process
       time.Sleep(100 * time.Millisecond)
       
       // Pod 2's index should now know about tool_a and tool_b
       assert.NotEmpty(t, index2.LookupTool("tool_a"))
       assert.NotEmpty(t, index2.LookupTool("tool_b"))
   }
   ```

5. **Helm validation**:
   ```bash
   helm lint charts/mcpgateway
   helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml --set ingress.enabled=true --set ingress.host=gw.test.com
   ```

### Verification
```bash
go test ./internal/registration/...
go vet ./...
helm lint charts/mcpgateway
helm template test charts/mcpgateway -f charts/mcpgateway/values.yaml
```

### Acceptance Criteria
- Broadcaster selected automatically based on available infrastructure
- Registration events flow from service → broadcaster → subscriber → index update
- Integration test proves Pod 2 sees Pod 1's registrations
- Ingress template renders correctly with TLS and annotations
- Helm lint passes with all value combinations
- No goroutine leaks on shutdown
