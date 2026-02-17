package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerStateTransitions(t *testing.T) {
	t.Parallel()

	testErr := errors.New("boom")

	tests := []struct {
		name         string
		setup        func(*CircuitBreaker)
		fn           func() error
		wantErr      error
		wantState    CircuitState
		wantFailures int
		wantCalls    int
	}{
		{
			name:         "closed success stays closed",
			setup:        func(cb *CircuitBreaker) {},
			fn:           func() error { return nil },
			wantState:    StateClosed,
			wantFailures: 0,
			wantCalls:    1,
		},
		{
			name: "closed failure below threshold increments failures",
			setup: func(cb *CircuitBreaker) {
				cb.threshold = 2
			},
			fn:           func() error { return testErr },
			wantErr:      testErr,
			wantState:    StateClosed,
			wantFailures: 1,
			wantCalls:    1,
		},
		{
			name: "closed failure reaches threshold opens",
			setup: func(cb *CircuitBreaker) {
				cb.threshold = 1
			},
			fn:           func() error { return testErr },
			wantErr:      testErr,
			wantState:    StateOpen,
			wantFailures: 1,
			wantCalls:    1,
		},
		{
			name: "open with timeout not elapsed returns sentinel",
			setup: func(cb *CircuitBreaker) {
				cb.state = StateOpen
				cb.lastFailure = time.Now()
				cb.timeout = time.Hour
			},
			fn:           func() error { return nil },
			wantErr:      ErrCircuitOpen,
			wantState:    StateOpen,
			wantFailures: 0,
			wantCalls:    0,
		},
		{
			name: "open with elapsed timeout probes and closes on success",
			setup: func(cb *CircuitBreaker) {
				cb.state = StateOpen
				cb.lastFailure = time.Now().Add(-2 * time.Second)
				cb.timeout = time.Second
				cb.failures = 3
			},
			fn:           func() error { return nil },
			wantState:    StateClosed,
			wantFailures: 0,
			wantCalls:    1,
		},
		{
			name: "open with elapsed timeout probes and reopens on failure",
			setup: func(cb *CircuitBreaker) {
				cb.state = StateOpen
				cb.lastFailure = time.Now().Add(-2 * time.Second)
				cb.timeout = time.Second
				cb.threshold = 4
			},
			fn:           func() error { return testErr },
			wantErr:      testErr,
			wantState:    StateOpen,
			wantFailures: 4,
			wantCalls:    1,
		},
		{
			name: "half-open success closes and resets",
			setup: func(cb *CircuitBreaker) {
				cb.state = StateHalfOpen
				cb.failures = 2
			},
			fn:           func() error { return nil },
			wantState:    StateClosed,
			wantFailures: 0,
			wantCalls:    1,
		},
		{
			name: "half-open failure reopens",
			setup: func(cb *CircuitBreaker) {
				cb.state = StateHalfOpen
				cb.threshold = 3
			},
			fn:           func() error { return testErr },
			wantErr:      testErr,
			wantState:    StateOpen,
			wantFailures: 3,
			wantCalls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker("svc-a", 3, 200*time.Millisecond)
			tt.setup(cb)

			calls := 0
			err := cb.Execute(context.Background(), func() error {
				calls++
				return tt.fn()
			})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			if cb.State() != tt.wantState {
				t.Fatalf("expected state %s, got %s", tt.wantState, cb.State())
			}
			if cb.Failures() != tt.wantFailures {
				t.Fatalf("expected failures %d, got %d", tt.wantFailures, cb.Failures())
			}
			if calls != tt.wantCalls {
				t.Fatalf("expected %d calls, got %d", tt.wantCalls, calls)
			}
		})
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker("svc-a", 1, time.Second)
	err := cb.Execute(context.Background(), func() error {
		return errors.New("failed")
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected open state, got %s", cb.State())
	}

	cb.Reset()
	if cb.State() != StateClosed {
		t.Fatalf("expected closed state after reset, got %s", cb.State())
	}
	if cb.Failures() != 0 {
		t.Fatalf("expected failures reset to 0, got %d", cb.Failures())
	}
}

func TestCircuitBreakerContextCancellation(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker("svc-a", 1, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := cb.Execute(ctx, func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if called {
		t.Fatal("expected function not to be called")
	}
}

func TestNewCircuitBreakerDefaultsAndGuards(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker("svc-a", 0, 0)
	if cb.threshold != 1 {
		t.Fatalf("expected threshold default 1, got %d", cb.threshold)
	}
	if cb.timeout <= 0 {
		t.Fatalf("expected positive timeout, got %v", cb.timeout)
	}

	var nilBreaker *CircuitBreaker
	err := nilBreaker.Execute(context.Background(), func() error { return nil })
	if err == nil {
		t.Fatal("expected nil breaker error")
	}

	err = cb.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected nil function error")
	}

	cb.state = CircuitState("invalid")
	err = cb.Execute(context.Background(), func() error { return nil })
	if err == nil {
		t.Fatal("expected invalid state error")
	}
}
