# Phase 11A: Distributed Rate Limiting with Redis/DragonflyDB

## Overview

Extract the rate limiter into a backend interface and implement a Redis/DragonflyDB-backed sliding window counter that enforces rate limits correctly across all gateway replicas. Include automatic fallback to in-memory rate limiting when Redis is unavailable.

## Scope

### Backend Interface (`internal/ratelimit/backend.go`)

Extract a `LimiterBackend` interface from the existing in-memory implementation:

```go
type LimiterBackend interface {
    // Allow checks if the request should be allowed under rate limits.
    // Returns: allowed (bool), remaining count, retryAfter duration.
    Allow(ctx context.Context, key LimiterKey, limit float64, burst int) (allowed bool, remaining int, retryAfter time.Duration, err error)
    
    // Close releases any resources held by the backend.
    Close() error
}
```

### Memory Backend (`internal/ratelimit/memory_backend.go`)

Refactor the existing in-memory token bucket logic into this file, implementing `LimiterBackend`:

- Move the existing `LimiterStore` internals (map of `rate.Limiter` entries) into `MemoryBackend`.
- `NewMemoryBackend(cleanupInterval, maxIdle time.Duration) *MemoryBackend`
- `Allow()` delegates to `rate.Limiter.Allow()` and computes remaining/retryAfter.
- Background cleanup goroutine for idle limiters (moved from existing cleanup.go).
- `Close()` stops the cleanup goroutine.

### Redis Backend (`internal/ratelimit/redis_backend.go`)

Implement distributed sliding window rate limiting using a Lua script for atomicity:

- `NewRedisBackend(redisURL string, opts ...RedisOption) (*RedisBackend, error)`
- Options: `WithFallback(fb LimiterBackend)` for automatic fallback, `WithKeyPrefix(prefix string)`.
- Redis key pattern: `rl:{client_id}:{route}:{window_epoch}` where `window_epoch` is `unix_timestamp / window_seconds`.
- **Lua script** (embedded via `//go:embed`):
  ```lua
  -- sliding_window.lua
  -- KEYS[1] = rate limit key
  -- ARGV[1] = current timestamp (microseconds)
  -- ARGV[2] = window size (microseconds)  
  -- ARGV[3] = max requests per window
  --
  -- Returns: {allowed (0/1), remaining, retry_after_ms}
  
  local key = KEYS[1]
  local now = tonumber(ARGV[1])
  local window = tonumber(ARGV[2])
  local limit = tonumber(ARGV[3])
  local window_start = now - window
  
  -- Remove entries outside the window
  redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)
  
  -- Count requests in current window
  local count = redis.call('ZCARD', key)
  
  if count < limit then
      -- Add this request with current timestamp as score
      -- Use timestamp + random suffix as member for uniqueness
      redis.call('ZADD', key, now, tostring(now) .. ':' .. tostring(math.random(1000000)))
      -- Set TTL to window size (auto-cleanup)
      redis.call('PEXPIRE', key, math.ceil(window / 1000))
      return {1, limit - count - 1, 0}
  else
      -- Calculate when the oldest entry in the window will expire
      local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
      local retry_after = 0
      if #oldest >= 2 then
          retry_after = math.ceil((tonumber(oldest[2]) + window - now) / 1000)
          if retry_after < 0 then retry_after = 0 end
      end
      return {0, 0, retry_after}
  end
  ```
- `Allow()` implementation:
  1. Build key from `LimiterKey`.
  2. Convert `limit` (requests/sec) and `burst` to window parameters: `windowMicros = int64(float64(burst) / limit * 1e6)`, `maxRequests = burst`.
  3. Execute Lua script via `redis.Client.EvalSha()` (pre-load script with `ScriptLoad`).
  4. Parse result array into `allowed`, `remaining`, `retryAfter`.
  5. On Redis error: if fallback is configured, delegate to fallback and log warning. If no fallback, return error.
- `Close()` closes the Redis client.

### Fallback Logic (`internal/ratelimit/redis_backend.go`)

The Redis backend wraps a fallback `LimiterBackend` (typically `MemoryBackend`):

```go
func (r *RedisBackend) Allow(ctx context.Context, key LimiterKey, limit float64, burst int) (bool, int, time.Duration, error) {
    allowed, remaining, retryAfter, err := r.evalLuaScript(ctx, key, limit, burst)
    if err != nil {
        r.logger.Warn("redis rate limit failed, falling back to memory",
            "error", err,
            "key", key.String(),
        )
        r.metrics.fallbackCount.Inc() // Prometheus counter
        if r.fallback != nil {
            return r.fallback.Allow(ctx, key, limit, burst)
        }
        // If no fallback, allow the request (fail-open) and log
        r.logger.Error("redis rate limit failed with no fallback, allowing request", "error", err)
        return true, burst, 0, nil
    }
    return allowed, remaining, retryAfter, nil
}
```

### NATS KV Backend (`internal/ratelimit/nats_backend.go`)

For Cruvero-integrated mode where NATS JetStream is available:

- `NewNATSBackend(js nats.JetStreamContext, opts ...NATSOption) (*NATSBackend, error)`
- Creates a KV bucket named `ratelimits` if it doesn't exist.
- Key: `{client_id}:{route}` — value: JSON `{count: N, window_start: timestamp}`.
- Uses NATS KV `Update()` with revision for CAS (compare-and-swap) consistency.
- Retry on CAS conflict (up to 3 attempts).
- Fallback to memory on persistent failure.
- `Close()` is a no-op (NATS client lifecycle managed elsewhere).

### Middleware Refactor (`internal/ratelimit/middleware.go`)

- Update `RateLimitMiddleware` to accept `LimiterBackend` instead of `*LimiterStore`.
- Use the `Allow()` return values to set accurate rate limit headers:
  ```go
  w.Header().Set("X-RateLimit-Limit", strconv.Itoa(burst))
  w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
  if !allowed {
      w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
      http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
      return
  }
  ```

### Config Changes (`internal/config/config.go`)

- Add `RateLimitBackend string` (default `"memory"`, options: `"memory"`, `"redis"`, `"nats"`).
- Add `RedisURL string` (required when backend is `"redis"`).
- Parse `MCPGW_RATE_LIMIT_BACKEND` and `MCPGW_REDIS_URL`.
- Validate: `"nats"` requires `CruveroEnabled=true`. `"redis"` requires non-empty `RedisURL`.

## Files Created or Modified

| File | Description |
|------|-------------|
| `internal/ratelimit/backend.go` | LimiterBackend interface definition |
| `internal/ratelimit/memory_backend.go` | Refactored in-memory implementation |
| `internal/ratelimit/memory_backend_test.go` | Tests |
| `internal/ratelimit/redis_backend.go` | Redis/DragonflyDB sliding window implementation |
| `internal/ratelimit/redis_backend_test.go` | Tests using miniredis |
| `internal/ratelimit/nats_backend.go` | NATS JetStream KV implementation |
| `internal/ratelimit/nats_backend_test.go` | Tests |
| `internal/ratelimit/lua/sliding_window.lua` | Embedded Lua script |
| `internal/ratelimit/middleware.go` | Refactored to use LimiterBackend interface |
| `internal/ratelimit/middleware_test.go` | Updated tests |
| `internal/ratelimit/store.go` | Deprecated or refactored into memory_backend.go |
| `internal/config/config.go` | Add rate limit backend and Redis config fields |
| `cmd/mcpgw/serve.go` | Backend selection logic |
| `go.mod` | Add redis/go-redis, miniredis |

## Testing Requirements

- Memory backend: all existing tests pass after refactor, cleanup still works
- Redis backend: test with miniredis — requests within limit allowed, over limit rejected, accurate remaining count, retry-after header, Lua script atomicity (concurrent goroutines)
- Redis fallback: mock Redis failure, verify fallback to memory backend, verify warning log
- NATS backend: test with mock JetStream — CAS consistency, retry on conflict
- Middleware: test with each backend type, verify headers are accurate
- Config: test all backend options and validation
- Integration: test that 3 concurrent "pods" (goroutines with separate middleware instances sharing the same Redis) correctly enforce a shared rate limit
- Coverage: >=80% on all new and modified packages
