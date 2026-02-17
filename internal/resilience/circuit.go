package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when calls are short-circuited while the breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker open")

// CircuitState describes the breaker state.
type CircuitState string

const (
	StateClosed   CircuitState = "closed"
	StateOpen     CircuitState = "open"
	StateHalfOpen CircuitState = "half_open"
)

// CircuitBreaker implements a basic closed/open/half-open state machine.
type CircuitBreaker struct {
	name        string
	state       CircuitState
	failures    int
	threshold   int
	timeout     time.Duration
	lastFailure time.Time
	mu          sync.Mutex
}

// NewCircuitBreaker creates a circuit breaker with sane defaults.
func NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 1
	}
	if timeout <= 0 {
		timeout = time.Second
	}

	return &CircuitBreaker{
		name:      name,
		state:     StateClosed,
		threshold: threshold,
		timeout:   timeout,
	}
}

// Execute runs fn according to circuit state rules.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	if cb == nil {
		return fmt.Errorf("execute with circuit breaker: circuit breaker is nil")
	}
	if fn == nil {
		return fmt.Errorf("execute with circuit breaker: function is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cb.mu.Lock()
	switch cb.state {
	case StateOpen:
		if time.Since(cb.lastFailure) < cb.timeout {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
		// Transition to half-open and allow a single probe call while lock is held.
		cb.state = StateHalfOpen
	case StateHalfOpen:
		// Keep lock held so only one probe request can run.
	case StateClosed:
		cb.mu.Unlock()
		return cb.executeClosed(ctx, fn)
	default:
		cb.mu.Unlock()
		return fmt.Errorf("execute with circuit breaker: unknown state %q", cb.state)
	}

	if err := ctx.Err(); err != nil {
		cb.mu.Unlock()
		return err
	}

	err := fn()
	if err != nil {
		cb.state = StateOpen
		cb.failures = cb.threshold
		cb.lastFailure = time.Now()
		cb.mu.Unlock()
		return err
	}

	cb.state = StateClosed
	cb.failures = 0
	cb.lastFailure = time.Time{}
	cb.mu.Unlock()
	return nil
}

func (cb *CircuitBreaker) executeClosed(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		if cb.failures >= cb.threshold {
			cb.state = StateOpen
			cb.lastFailure = time.Now()
		}
		return err
	}

	cb.failures = 0
	return nil
}

// State returns the current breaker state.
func (cb *CircuitBreaker) State() CircuitState {
	if cb == nil {
		return StateOpen
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Reset forces closed state and clears failure counters.
func (cb *CircuitBreaker) Reset() {
	if cb == nil {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.lastFailure = time.Time{}
}

// Failures returns the current failure count.
func (cb *CircuitBreaker) Failures() int {
	if cb == nil {
		return 0
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failures
}
