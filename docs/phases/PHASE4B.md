# Phase 4B: Circuit Breaker, Retry, Connection Pooling

## Overview

Implement resilience patterns for backend communication: circuit breakers to avoid cascading failures, retry with exponential backoff for transient errors, and connection pooling for efficient backend connections.

## Scope

### Circuit Breaker (`internal/resilience/circuit.go`)
- `CircuitBreaker` struct: state (closed/open/half-open), failure count, threshold, timeout, last failure time, mutex
- States:
  - Closed: requests pass through, failures increment counter
  - Open: requests immediately fail with circuit open error, timer running
  - Half-open: one probe request allowed, success -> closed, failure -> open
- `NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker`
- `Execute(ctx context.Context, fn func() error) error`: wrap function with circuit breaker logic
- `State() CircuitState`: return current state
- `Reset()`: force reset to closed
- Configurable via MCPGW_CIRCUIT_THRESHOLD (default 5) and MCPGW_CIRCUIT_TIMEOUT (default 30s)

### Circuit Breaker Registry (`internal/resilience/registry.go`)
- `BreakerRegistry` struct: map of serverID -> *CircuitBreaker, mutex
- `GetOrCreate(serverID string, threshold int, timeout time.Duration) *CircuitBreaker`
- `Remove(serverID string)`: remove breaker when server deregisters
- `ListOpen() []string`: return IDs of servers with open circuits (for health reporting)

### Retry (`internal/resilience/retry.go`)
- `RetryConfig` struct: MaxAttempts int, InitialBackoff time.Duration, MaxBackoff time.Duration, BackoffMultiplier float64, Jitter bool
- `Retry(ctx context.Context, cfg RetryConfig, fn func() error) error`:
  - Execute fn up to MaxAttempts times
  - On failure, wait with exponential backoff: delay = min(initial * multiplier^attempt, max)
  - Add jitter: +/-25% randomization
  - Respect context cancellation
  - Return last error if all attempts fail
- `IsRetryable(err error) bool`: classify errors (timeout, 503, connection refused -> retryable; 4xx -> not retryable)
- Default config from env: MCPGW_RETRY_MAX (default 3)

### Connection Pool Integration (`internal/resilience/pool.go`)
- Per-backend http.Transport configuration:
  - MaxIdleConnsPerHost (configurable, default 10)
  - IdleConnTimeout (configurable, default 90s)
  - TLSHandshakeTimeout (10s)
  - ResponseHeaderTimeout (configurable)
- `NewTransport(tlsConfig *tls.Config, opts PoolOptions) *http.Transport`
- PoolOptions struct: MaxIdlePerHost, IdleTimeout, HandshakeTimeout, ResponseTimeout

### Resilient Client Wrapper (`internal/resilience/client.go`)
- `ResilientClient` wrapping BackendClient with circuit breaker + retry
- `NewResilientClient(backend *proxy.BackendClient, breaker *CircuitBreaker, retryCfg RetryConfig) *ResilientClient`
- CallTool method: retry(circuit(backend.CallTool))
- Compose: retry wraps circuit breaker wraps actual call

## Files Created

| File | Description |
|------|-------------|
| internal/resilience/circuit.go | Circuit breaker implementation |
| internal/resilience/registry.go | Circuit breaker registry |
| internal/resilience/retry.go | Retry with exponential backoff |
| internal/resilience/pool.go | Connection pool configuration |
| internal/resilience/client.go | Resilient client wrapper |
| internal/resilience/circuit_test.go | Circuit breaker tests |
| internal/resilience/registry_test.go | Registry tests |
| internal/resilience/retry_test.go | Retry tests |
| internal/resilience/pool_test.go | Pool tests |
| internal/resilience/client_test.go | Resilient client tests |

## Testing Requirements

- Circuit breaker: test closed->open on threshold failures, test open->half-open after timeout, test half-open->closed on success, test half-open->open on failure
- Registry: test GetOrCreate, test Remove, test ListOpen
- Retry: test success on first attempt, test success on nth attempt, test all attempts fail, test context cancellation stops retry, test backoff timing, test jitter
- Pool: test transport configuration
- Resilient client: test composition of retry + circuit breaker + backend
- Coverage: >=80%
