# Phase 5A Implementation Prompts

## Prompt 1 of 3: Limiter Store

### Required Reading (read these files before writing code)
- docs/phases/PHASE5A.md
- internal/types/types.go (PolicyProfile)
- internal/config/config.go (MCPGW_RATE_DEFAULT, MCPGW_RATE_BURST)

### Task

Create the token bucket limiter store.

1. `internal/ratelimit/store.go`:
   - Define LimiterKey struct with ClientID and Route fields
   - Define entry struct with limiter *rate.Limiter and lastUsed time.Time
   - Define LimiterStore struct with sync.RWMutex, map[LimiterKey]*entry, default rate and burst
   - NewLimiterStore(defaultRate float64, defaultBurst int) *LimiterStore
   - GetOrCreate(key LimiterKey, profile *types.PolicyProfile) *rate.Limiter:
     - RLock check for existing entry, update lastUsed, return if found
     - Lock for creation if not found
     - Use profile rate/burst if non-zero, otherwise defaults
   - Remove(key LimiterKey): Lock + delete
   - Cleanup(maxIdle time.Duration): Lock, iterate, delete entries with lastUsed before threshold, return count
   - Count() int: return number of active limiters

2. `internal/ratelimit/store_test.go`:
   - Test GetOrCreate creates new limiter
   - Test GetOrCreate returns existing limiter for same key
   - Test different keys get independent limiters
   - Test Remove deletes entry
   - Test Cleanup removes idle entries, preserves active ones
   - Test Count reflects current state
   - Test concurrent access safety (use goroutines)

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Thread-safe limiter management
- Profile-specific rates applied correctly
- Cleanup prevents memory leaks

---

## Prompt 2 of 3: Rate Limit Middleware

### Required Reading (read these files before writing code)
- docs/phases/PHASE5A.md
- internal/ratelimit/store.go
- internal/identity/context.go

### Task

Create rate limiting middleware and profile resolver.

1. `internal/ratelimit/resolver.go`:
   - Define ProfileResolver interface: Resolve(identity *identity.Identity) *types.PolicyProfile
   - Define DefaultProfileResolver struct: profiles map, default profile
   - NewDefaultProfileResolver(profiles map[string]*types.PolicyProfile, defaultProfile *types.PolicyProfile) *DefaultProfileResolver
   - Resolve: check identity metadata for "policy_profile" key, look up in map, fall back to default

2. `internal/ratelimit/middleware.go`:
   - RateLimitMiddleware(store *LimiterStore, resolver ProfileResolver, logger *slog.Logger) func(http.Handler) http.Handler:
     - Extract identity from context (if none, use "anonymous" key)
     - Resolve profile
     - Build LimiterKey from identity.ID and request route
     - Call store.GetOrCreate
     - If limiter.Allow(): set headers, call next
     - If not: set headers, write 429 response with JSON body and Retry-After header
   - setRateLimitHeaders helper: compute and set X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset

3. Tests:
   - `internal/ratelimit/resolver_test.go`: Test profile resolution with various identities
   - `internal/ratelimit/middleware_test.go`: httptest-based tests for 200 and 429 responses, header verification

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Middleware correctly applies rate limits
- Proper 429 response with headers
- Profile resolver maps identities to profiles

---

## Prompt 3 of 3: Background Cleanup & Integration

### Required Reading (read these files before writing code)
- internal/ratelimit/store.go
- internal/ratelimit/middleware.go
- internal/server/server.go

### Task

Implement background cleanup and wire rate limiting into the server.

1. `internal/ratelimit/cleanup.go`:
   - StartCleanup(ctx context.Context, store *LimiterStore, interval, maxIdle time.Duration):
     - Create ticker with interval
     - On each tick: call store.Cleanup(maxIdle), log count if > 0
     - Stop on ctx.Done()

2. Wire into server startup:
   - Create LimiterStore in server initialization
   - Start cleanup goroutine
   - Add RateLimitMiddleware to router middleware chain (after auth, before handlers)

3. `internal/ratelimit/cleanup_test.go`:
   - Test cleanup runs periodically
   - Test cleanup stops on context cancellation
   - Test idle entries removed after maxIdle

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Cleanup runs on schedule and removes idle limiters
- Rate limiting is active in the server middleware chain
- Clean shutdown via context cancellation
- >=80% coverage across ratelimit package
