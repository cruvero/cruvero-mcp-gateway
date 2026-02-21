# Phase 4B Implementation Prompts

## Prompt 1 of 3: Circuit Breaker

### Required Reading (read these files before writing code)
- docs/phases/PHASE4B.md
- internal/proxy/client.go
- internal/config/config.go

### Task

Implement the circuit breaker pattern.

1. `internal/resilience/circuit.go`:
   - Define CircuitState type: StateClosed, StateOpen, StateHalfOpen
   - Define CircuitBreaker struct: name, state, failures, threshold, timeout, lastFailure, mu sync.Mutex
   - NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker
   - Execute(ctx context.Context, fn func() error) error:
     - If open and timeout not elapsed -> return ErrCircuitOpen
     - If open and timeout elapsed -> transition to half-open, allow one request
     - If half-open and fn succeeds -> transition to closed, reset failures
     - If half-open and fn fails -> transition to open, reset timer
     - If closed and fn fails -> increment failures, if >= threshold -> transition to open
     - If closed and fn succeeds -> reset failures
   - State() CircuitState, Reset(), Failures() int
   - Define ErrCircuitOpen sentinel error

2. `internal/resilience/registry.go`:
   - BreakerRegistry with sync.RWMutex + map
   - GetOrCreate, Remove, ListOpen methods

3. Tests:
   - `internal/resilience/circuit_test.go`: Table-driven tests for all state transitions
   - `internal/resilience/registry_test.go`: Test concurrent GetOrCreate, Remove

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- All three states and transitions work correctly
- Thread-safe operation
- Clear sentinel error for open circuit

---

## Prompt 2 of 3: Retry & Connection Pool

### Required Reading (read these files before writing code)
- docs/phases/PHASE4B.md
- internal/resilience/circuit.go
- internal/config/config.go

### Task

Implement retry logic and connection pool configuration.

1. `internal/resilience/retry.go`:
   - Define RetryConfig struct with fields and defaults
   - DefaultRetryConfig() RetryConfig: from env vars with fallbacks
   - Retry(ctx context.Context, cfg RetryConfig, fn func() error) error:
     - Loop up to MaxAttempts
     - On success, return nil
     - On failure, check IsRetryable; if not, return immediately
     - Calculate backoff: initial * multiplier^attempt, capped at max
     - Add jitter if enabled: +/-25% via crypto/rand
     - Sleep with context awareness (select on timer and ctx.Done())
     - Return last error wrapped with attempt count
   - IsRetryable(err error) bool: check for net.Error (timeout), specific HTTP status codes

2. `internal/resilience/pool.go`:
   - Define PoolOptions struct: MaxIdlePerHost, IdleTimeout, HandshakeTimeout, ResponseTimeout
   - DefaultPoolOptions() PoolOptions
   - NewTransport(tlsConfig *tls.Config, opts PoolOptions) *http.Transport

3. Tests:
   - `internal/resilience/retry_test.go`: Test immediate success, test retry then success, test all fail, test non-retryable error stops early, test context cancellation, test backoff increases
   - `internal/resilience/pool_test.go`: Test transport creation with options

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Retry respects context cancellation
- Backoff is exponential with jitter
- Non-retryable errors fail fast
- Transport configured correctly

---

## Prompt 3 of 3: Resilient Client Wrapper

### Required Reading (read these files before writing code)
- docs/phases/PHASE4B.md
- internal/resilience/circuit.go
- internal/resilience/retry.go
- internal/proxy/client.go
- internal/proxy/router.go

### Task

Create the resilient client wrapper and integrate with the proxy router.

1. `internal/resilience/client.go`:
   - Define ResilientClient struct: backend (interface for testability), breaker *CircuitBreaker, retryCfg RetryConfig, logger
   - Define BackendCaller interface: CallTool(ctx, name, args) (*proxy.ToolResult, error)
   - NewResilientClient(backend BackendCaller, breaker *CircuitBreaker, retryCfg RetryConfig, logger *slog.Logger) *ResilientClient
   - CallTool(ctx, name, args): Retry wrapping CircuitBreaker.Execute wrapping backend.CallTool
   - Log each attempt, circuit state changes

2. Integrate with proxy router:
   - Update proxy.Router to use ResilientClient instead of raw BackendClient
   - When creating clients, wrap with circuit breaker and retry

3. `internal/resilience/client_test.go`:
   - Mock BackendCaller
   - Test call succeeds on first attempt
   - Test call retries transient failure then succeeds
   - Test circuit opens after threshold failures
   - Test circuit open returns error immediately (no backend call)
   - Test circuit half-open recovery

### Verification
```bash
# Run checks relevant to the files changed in this prompt
go test ./...
go vet ./...
```

### Acceptance criteria
- Composition: retry -> circuit breaker -> backend call
- Circuit breaker prevents calls to unhealthy backends
- Retry handles transient failures
- >=80% coverage across resilience package
