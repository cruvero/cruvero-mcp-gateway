# Phase 11A Implementation Prompts

## Prompt 1 of 3: Backend Interface & Memory Backend Refactor

### Required Reading (read these files before writing code)
- docs/phases/PHASE11A.md (full scope)
- internal/ratelimit/store.go (current in-memory implementation)
- internal/ratelimit/middleware.go (current middleware, references to LimiterStore)
- internal/ratelimit/cleanup.go (background cleanup logic)
- LLM.md (conventions)

### Task

Extract the `LimiterBackend` interface and refactor the existing in-memory logic into a clean backend implementation.

1. `internal/ratelimit/backend.go`:
   ```go
   package ratelimit
   
   import (
       "context"
       "time"
   )
   
   // LimiterBackend abstracts rate limit storage and enforcement.
   // Implementations must be safe for concurrent use.
   type LimiterBackend interface {
       // Allow checks if the request should be allowed under rate limits.
       // limit is requests per second, burst is the maximum burst size.
       // Returns: allowed, remaining tokens, retryAfter (0 if allowed), error.
       Allow(ctx context.Context, key LimiterKey, limit float64, burst int) (allowed bool, remaining int, retryAfter time.Duration, err error)
       
       // Close releases any resources (connections, goroutines) held by the backend.
       Close() error
   }
   ```

2. `internal/ratelimit/memory_backend.go`:
   - Move the existing `LimiterStore` internals into `MemoryBackend` struct.
   - `MemoryBackend` struct:
     ```go
     type MemoryBackend struct {
         mu       sync.RWMutex
         limiters map[string]*memoryEntry // key: LimiterKey.String()
         stopCh   chan struct{}
     }
     
     type memoryEntry struct {
         limiter  *rate.Limiter
         lastUsed time.Time
     }
     ```
   - `NewMemoryBackend(cleanupInterval, maxIdle time.Duration) *MemoryBackend`:
     - Start background cleanup goroutine.
     - Return backend.
   - `Allow(ctx, key, limit, burst)`:
     - Build string key from `LimiterKey`.
     - Lock, get-or-create `rate.Limiter` with the given limit and burst.
     - Call `limiter.Allow()`.
     - Compute `remaining`: approximate via `limiter.Tokens()` (cast to int).
     - Compute `retryAfter`: if not allowed, use `limiter.Reserve().Delay()` (cancel the reservation immediately after getting the delay).
     - Update `lastUsed`.
     - Return results.
   - Background cleanup: iterate map, remove entries where `time.Since(lastUsed) > maxIdle`.
   - `Close()`: signal stop channel, wait for cleanup goroutine to exit.

3. **Refactor** `internal/ratelimit/middleware.go`:
   - Change `RateLimitMiddleware` signature to accept `LimiterBackend` instead of `*LimiterStore`:
     ```go
     func RateLimitMiddleware(backend LimiterBackend, resolver ProfileResolver, logger *slog.Logger) func(http.Handler) http.Handler
     ```
   - Update the middleware body to call `backend.Allow()` and use the returned `remaining` and `retryAfter` for headers.
   - Set headers:
     ```go
     w.Header().Set("X-RateLimit-Limit", strconv.Itoa(burst))
     w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
     if retryAfter > 0 {
         w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
     }
     ```

4. **Remove or deprecate** `internal/ratelimit/store.go` and `internal/ratelimit/cleanup.go` — their logic is now in `memory_backend.go`. If other code references them, update imports. If they're used elsewhere, keep thin wrappers that delegate to the memory backend.

5. **Update** `cmd/mcpgw/serve.go`:
   - Replace `NewLimiterStore(...)` with `NewMemoryBackend(...)`.
   - Pass the backend to `RateLimitMiddleware`.
   - This prompt is memory-only — Redis wiring comes in Prompt 2.

6. **Tests:**
   - `internal/ratelimit/memory_backend_test.go`:
     - Test Allow within limit: returns `allowed=true`, `remaining > 0`.
     - Test Allow over limit: returns `allowed=false`, `remaining=0`, `retryAfter > 0`.
     - Test same key same limiter: two calls with same key share the same limiter instance.
     - Test different keys: independent limiters.
     - Test cleanup: create entry, wait beyond maxIdle, verify removed.
     - Test Close stops cleanup goroutine (no goroutine leak).
   - `internal/ratelimit/middleware_test.go`:
     - Update existing tests to use `MemoryBackend` (or mock `LimiterBackend`).
     - Test rate limit headers are present and accurate.
     - Test 429 response includes `Retry-After`.

### Verification
```bash
go test ./internal/ratelimit/...
go vet ./...
```

### Acceptance Criteria
- All existing rate limit tests pass after refactor
- MemoryBackend implements LimiterBackend interface
- Middleware works with the interface (not a concrete type)
- Cleanup goroutine stops cleanly on Close()
- No breaking changes to serve.go behavior
- Headers are accurate

---

## Prompt 2 of 3: Redis/DragonflyDB Backend with Lua Script

### Required Reading (read these files before writing code)
- docs/phases/PHASE11A.md (Redis backend and Lua script sections)
- internal/ratelimit/backend.go (LimiterBackend interface)
- internal/ratelimit/memory_backend.go (fallback implementation)

### Task

Implement the Redis-backed distributed rate limiter with a Lua sliding window script and automatic fallback.

1. **Add dependencies** to `go.mod`:
   ```bash
   go get github.com/redis/go-redis/v9
   go get github.com/alicebob/miniredis/v2
   ```

2. **Lua script** `internal/ratelimit/lua/sliding_window.lua`:
   Create this file with the exact Lua script from the PHASE11A.md spec. Use `//go:embed` to load it:
   ```go
   //go:embed lua/sliding_window.lua
   var slidingWindowScript string
   ```

3. `internal/ratelimit/redis_backend.go`:
   ```go
   type RedisBackend struct {
       client     *redis.Client
       scriptSHA  string
       keyPrefix  string
       fallback   LimiterBackend
       logger     *slog.Logger
       mu         sync.RWMutex
       healthy    bool
       healthCh   chan struct{} // closed when health check goroutine should stop
   }
   
   type RedisOption func(*RedisBackend)
   
   func WithFallback(fb LimiterBackend) RedisOption
   func WithKeyPrefix(prefix string) RedisOption
   func WithLogger(logger *slog.Logger) RedisOption
   ```
   
   - `NewRedisBackend(redisURL string, opts ...RedisOption) (*RedisBackend, error)`:
     - Parse URL with `redis.ParseURL()`.
     - Create `redis.Client`.
     - Ping to verify connectivity.
     - Load Lua script with `ScriptLoad()` and store the SHA.
     - Start health check goroutine (ping every 10s, update `healthy` flag).
     - Set `healthy = true`.
   
   - `Allow(ctx, key, limit, burst)`:
     - Build Redis key: `{keyPrefix}rl:{clientID}:{route}`.
     - Calculate window parameters:
       ```go
       // Window = burst / limit seconds (e.g., burst=20, limit=10 → 2s window)
       windowMicros := int64(float64(burst) / limit * 1e6)
       nowMicros := time.Now().UnixMicro()
       ```
     - Execute: `r.client.EvalSha(ctx, r.scriptSHA, []string{redisKey}, nowMicros, windowMicros, burst)`.
     - Parse result: `[]interface{}` → `[allowed int64, remaining int64, retryAfterMs int64]`.
     - On error: call `r.handleError(ctx, err, key, limit, burst)`.
   
   - `handleError(ctx, err, key, limit, burst)`:
     - If `fallback != nil`: delegate to fallback, log at Warn level.
     - If `fallback == nil`: allow the request (fail-open), log at Error level.
     - Increment a Prometheus counter for fallback events.
   
   - Health check goroutine:
     ```go
     func (r *RedisBackend) healthCheck() {
         ticker := time.NewTicker(10 * time.Second)
         defer ticker.Stop()
         for {
             select {
             case <-r.healthCh:
                 return
             case <-ticker.C:
                 ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
                 err := r.client.Ping(ctx).Err()
                 cancel()
                 r.mu.Lock()
                 wasHealthy := r.healthy
                 r.healthy = err == nil
                 r.mu.Unlock()
                 if wasHealthy && err != nil {
                     r.logger.Error("redis became unhealthy", "error", err)
                 } else if !wasHealthy && err == nil {
                     r.logger.Info("redis connection restored")
                     // Re-load script in case Redis restarted
                     r.reloadScript()
                 }
             }
         }
     }
     ```
   
   - `Close()`: close healthCh, close Redis client, close fallback if non-nil.

4. **Config** in `internal/config/config.go`:
   - Add `RateLimitBackend string` field (default `"memory"`).
   - Add `RedisURL string` field.
   - Parse `MCPGW_RATE_LIMIT_BACKEND` (accept: `"memory"`, `"redis"`, `"nats"`).
   - Parse `MCPGW_REDIS_URL`.
   - Validate: if `RateLimitBackend == "redis"`, `RedisURL` must be non-empty. If `"nats"`, `CruveroEnabled` must be true.

5. **Wiring** in `cmd/mcpgw/serve.go`:
   ```go
   var rlBackend ratelimit.LimiterBackend
   switch cfg.RateLimitBackend {
   case "redis":
       memFallback := ratelimit.NewMemoryBackend(5*time.Minute, 10*time.Minute)
       rlBackend, err = ratelimit.NewRedisBackend(cfg.RedisURL,
           ratelimit.WithFallback(memFallback),
           ratelimit.WithKeyPrefix("mcpgw:"),
           ratelimit.WithLogger(logger),
       )
       if err != nil {
           return fmt.Errorf("redis rate limit backend: %w", err)
       }
   case "nats":
       // Implemented in Phase 11A Prompt 3
       rlBackend, err = ratelimit.NewNATSBackend(js)
       if err != nil {
           return fmt.Errorf("nats rate limit backend: %w", err)
       }
   default:
       rlBackend = ratelimit.NewMemoryBackend(5*time.Minute, 10*time.Minute)
   }
   defer rlBackend.Close()
   ```

6. **Tests** `internal/ratelimit/redis_backend_test.go`:
   - Use `miniredis.Run()` for all tests (no real Redis needed).
   - Test: single request within limit → allowed.
   - Test: burst requests up to limit → all allowed, next one rejected.
   - Test: wait for window to pass → requests allowed again.
   - Test: remaining count is accurate.
   - Test: retryAfter is positive when rejected.
   - Test: concurrent goroutines (simulate 3 pods) sharing same miniredis → global limit enforced.
     ```go
     func TestRedisBackend_MultiPod(t *testing.T) {
         mr, _ := miniredis.Run()
         defer mr.Close()
         
         // 3 backends simulating 3 pods, all pointing to same Redis
         backends := make([]*RedisBackend, 3)
         for i := range backends {
             backends[i], _ = NewRedisBackend("redis://"+mr.Addr())
         }
         
         key := LimiterKey{ClientID: "user1", Route: "/mcp"}
         limit := 10.0  // 10 req/s
         burst := 10
         
         allowed := int32(0)
         var wg sync.WaitGroup
         // Fire 30 requests across 3 "pods"
         for _, b := range backends {
             for j := 0; j < 10; j++ {
                 wg.Add(1)
                 go func(backend *RedisBackend) {
                     defer wg.Done()
                     ok, _, _, _ := backend.Allow(context.Background(), key, limit, burst)
                     if ok {
                         atomic.AddInt32(&allowed, 1)
                     }
                 }(b)
             }
         }
         wg.Wait()
         
         // Exactly 10 should be allowed (burst size), not 30
         assert.Equal(t, int32(10), allowed)
     }
     ```
   - Test: Redis down with fallback → requests allowed via memory backend, warning logged.
   - Test: Redis down without fallback → requests allowed (fail-open), error logged.
   - Test: script reload after Redis restart.

### Verification
```bash
go test ./internal/ratelimit/... ./internal/config/...
go vet ./...
```

### Acceptance Criteria
- Lua script enforces sliding window correctly
- Multi-pod test proves global rate limiting works
- Fallback to memory is seamless (no 500s)
- Health check detects Redis failure and recovery
- Script is reloaded after Redis restart
- Config validation rejects invalid backend combinations
- All headers accurate

---

## Prompt 3 of 3: NATS KV Backend & Helm Values

### Required Reading (read these files before writing code)
- docs/phases/PHASE11A.md (NATS KV backend section)
- internal/ratelimit/backend.go (LimiterBackend interface)
- internal/events/client.go (existing NATS client)
- charts/mcpgateway/values.yaml (existing Helm values pattern)

### Task

Implement the NATS JetStream KV rate limit backend (for Cruvero mode) and add Helm values for Redis/backend configuration.

1. `internal/ratelimit/nats_backend.go`:
   ```go
   type NATSBackend struct {
       kv       nats.KeyValue
       fallback LimiterBackend
       logger   *slog.Logger
   }
   
   func NewNATSBackend(js nats.JetStreamContext, opts ...NATSOption) (*NATSBackend, error) {
       kv, err := js.KeyValue("ratelimits")
       if err == nats.ErrBucketNotFound {
           kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
               Bucket:  "ratelimits",
               TTL:     10 * time.Minute, // Auto-expire idle entries
               Storage: nats.MemoryStorage,
           })
       }
       if err != nil {
           return nil, fmt.Errorf("nats kv bucket: %w", err)
       }
       return &NATSBackend{kv: kv, logger: logger}, nil
   }
   ```
   
   - `Allow(ctx, key, limit, burst)`:
     - Build key: `{clientID}:{route}`.
     - Attempt CAS loop (max 3 retries):
       1. `kv.Get(key)` → get current entry (JSON: `{"count": N, "window_start": timestamp}`).
       2. If not found, create new entry with count=1.
       3. If found and window has not expired, check count < limit. If allowed, increment count.
       4. If window expired, reset to count=1 with new window_start.
       5. `kv.Update(key, newValue, revision)` → CAS update. If conflict (wrong revision), retry from step 1.
     - On persistent failure (3 retries): fall back to memory.
   
   - `Close()`: no-op (NATS lifecycle managed by events client).

2. **Helm values** additions to `charts/mcpgateway/values.yaml`:
   ```yaml
   rateLimit:
     backend: memory  # memory | redis | nats
   
   redis:
     enabled: false
     url: ""
     # For standalone mode distributed rate limiting.
     # DragonflyDB recommended (BSD license, Redis-compatible).
   ```
   
   Add to `charts/mcpgateway/templates/configmap.yaml`:
   ```yaml
   MCPGW_RATE_LIMIT_BACKEND: {{ .Values.rateLimit.backend | quote }}
   {{- if .Values.redis.enabled }}
   MCPGW_REDIS_URL: {{ .Values.redis.url | quote }}
   {{- end }}
   ```
   
   Add `values-prod.yaml` overrides:
   ```yaml
   rateLimit:
     backend: redis
   redis:
     enabled: true
     url: "redis://dragonfly:6379"
   ```

3. **Tests** `internal/ratelimit/nats_backend_test.go`:
   - Use embedded NATS server with JetStream enabled for tests.
   - Test: single request allowed.
   - Test: burst exhausted → rejected.
   - Test: window expiry → allowed again.
   - Test: CAS conflict (simulate concurrent update) → retry succeeds.
   - Test: bucket auto-created if not exists.

4. **Helm tests**: Update `charts/mcpgateway/` templates to include the new env vars. Run `helm template` validation:
   ```bash
   helm template charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-dev.yaml
   helm template charts/mcpgateway -f charts/mcpgateway/values.yaml -f charts/mcpgateway/values-prod.yaml
   ```

### Verification
```bash
go test ./internal/ratelimit/...
go vet ./...
helm lint charts/mcpgateway
helm template charts/mcpgateway -f charts/mcpgateway/values.yaml
```

### Acceptance Criteria
- NATS KV backend implements LimiterBackend interface correctly
- CAS loop handles concurrent access without data loss
- Helm values render correctly for all environments
- Backend selection in serve.go covers all three backends
- NATS backend auto-creates KV bucket on first use
