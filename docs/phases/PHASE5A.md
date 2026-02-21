# Phase 5A: Rate Limiting Engine

## Overview

Implement a per-client, per-route token bucket rate limiter using golang.org/x/time/rate. Rate limits are configurable per policy profile and enforced as HTTP middleware.

## Scope

### Rate Limiter Store (`internal/ratelimit/store.go`)
- `LimiterKey` struct: ClientID string, Route string
- `LimiterStore` struct: mu sync.RWMutex, limiters map[LimiterKey]*entry, defaultRate rate.Limit, defaultBurst int
- entry struct: limiter *rate.Limiter, lastUsed time.Time
- `NewLimiterStore(defaultRate float64, defaultBurst int) *LimiterStore`
- `GetOrCreate(key LimiterKey, profile *types.PolicyProfile) *rate.Limiter`:
  - If exists and not expired, return existing
  - Otherwise create with profile's rate/burst (or defaults)
  - Update lastUsed timestamp
- `Remove(key LimiterKey)`: explicit removal
- `Cleanup(maxIdle time.Duration)`: remove entries not used within maxIdle

### Rate Limit Middleware (`internal/ratelimit/middleware.go`)
- `RateLimitMiddleware(store *LimiterStore, profileResolver ProfileResolver, logger *slog.Logger) func(http.Handler) http.Handler`
- ProfileResolver interface: `Resolve(identity *identity.Identity) *types.PolicyProfile`
- Middleware logic:
  1. Extract identity from context
  2. Resolve policy profile for the identity
  3. Determine route key (URL path template or MCP method)
  4. Get or create limiter for (clientID, route)
  5. If limiter.Allow() -> pass through, set rate limit headers
  6. If not allowed -> return 429 with Retry-After header and rate limit headers
- Rate limit headers:
  - X-RateLimit-Limit: requests per second
  - X-RateLimit-Remaining: tokens remaining (approximation)
  - X-RateLimit-Reset: time until bucket refills (Unix timestamp)

### Background Cleanup (`internal/ratelimit/cleanup.go`)
- `StartCleanup(ctx context.Context, store *LimiterStore, interval, maxIdle time.Duration)`
- Periodic goroutine that calls store.Cleanup(maxIdle)
- Logs cleanup count
- Stops on context cancellation

### Policy Profile Resolver (`internal/ratelimit/resolver.go`)
- `DefaultProfileResolver` struct: profiles map[string]*types.PolicyProfile, defaultProfile *types.PolicyProfile
- Resolve: match identity scopes/metadata to a profile name, fall back to default
- Profiles loaded from config or store

## Files Created

| File | Description |
|------|-------------|
| internal/ratelimit/store.go | Limiter store with per-key token buckets |
| internal/ratelimit/middleware.go | Rate limit HTTP middleware |
| internal/ratelimit/cleanup.go | Background limiter cleanup |
| internal/ratelimit/resolver.go | Policy profile resolver |
| internal/ratelimit/store_test.go | Store tests |
| internal/ratelimit/middleware_test.go | Middleware tests |
| internal/ratelimit/cleanup_test.go | Cleanup tests |
| internal/ratelimit/resolver_test.go | Resolver tests |

## Testing Requirements

- Store: test GetOrCreate returns same limiter for same key, test different keys get different limiters, test Cleanup removes idle entries
- Middleware: test request within limit passes (200), test request over limit returns 429, test rate limit headers present, test different profiles get different rates
- Cleanup: test entries removed after idle period, test active entries preserved
- Resolver: test profile matching, test default fallback
- Coverage: >=80%
