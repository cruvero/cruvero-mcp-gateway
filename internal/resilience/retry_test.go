package resilience

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type statusErr struct {
	code int
}

func (e statusErr) Error() string    { return fmt.Sprintf("status %d", e.code) }
func (e statusErr) StatusCode() int  { return e.code }

func TestRetryImmediateSuccess(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := Retry(context.Background(), RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    5 * time.Millisecond,
		MaxBackoff:        20 * time.Millisecond,
		BackoffMultiplier: 2,
	}, func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := Retry(context.Background(), RetryConfig{
		MaxAttempts:       4,
		InitialBackoff:    5 * time.Millisecond,
		MaxBackoff:        30 * time.Millisecond,
		BackoffMultiplier: 2,
		Jitter:            false,
	}, func() error {
		attempts++
		if attempts < 3 {
			return timeoutErr{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryAllAttemptsFail(t *testing.T) {
	t.Parallel()

	target := timeoutErr{}
	attempts := 0
	err := Retry(context.Background(), RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    2 * time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		BackoffMultiplier: 2,
		Jitter:            false,
	}, func() error {
		attempts++
		return target
	})
	if err == nil {
		t.Fatal("expected retry failure")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if !errors.Is(err, target) {
		t.Fatalf("expected wrapped target error, got %v", err)
	}
}

func TestRetryStopsOnNonRetryable(t *testing.T) {
	t.Parallel()

	target := errors.New("bad request")
	attempts := 0
	err := Retry(context.Background(), RetryConfig{
		MaxAttempts:       5,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        4 * time.Millisecond,
		BackoffMultiplier: 2,
	}, func() error {
		attempts++
		return target
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
	if !errors.Is(err, target) {
		t.Fatalf("expected wrapped target error, got %v", err)
	}
}

func TestRetryContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	attempts := 0
	err := Retry(ctx, RetryConfig{
		MaxAttempts:       10,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        200 * time.Millisecond,
		BackoffMultiplier: 2,
		Jitter:            false,
	}, func() error {
		attempts++
		return timeoutErr{}
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if attempts < 1 {
		t.Fatalf("expected at least one attempt, got %d", attempts)
	}
}

func TestCalculateBackoffIncreases(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		MaxAttempts:       5,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        80 * time.Millisecond,
		BackoffMultiplier: 2,
		Jitter:            false,
	}

	b1 := calculateBackoff(cfg, 1)
	b2 := calculateBackoff(cfg, 2)
	b3 := calculateBackoff(cfg, 3)
	b4 := calculateBackoff(cfg, 4)
	b5 := calculateBackoff(cfg, 5)

	if !(b1 < b2 && b2 < b3 && b3 < b4) {
		t.Fatalf("expected increasing backoff, got %v %v %v %v", b1, b2, b3, b4)
	}
	if b5 != cfg.MaxBackoff {
		t.Fatalf("expected capped backoff %v, got %v", cfg.MaxBackoff, b5)
	}
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "timeout net error", err: timeoutErr{}, want: true},
		{name: "http 503", err: statusErr{code: 503}, want: true},
		{name: "http 429", err: statusErr{code: 429}, want: true},
		{name: "http 400", err: statusErr{code: 400}, want: false},
		{name: "connection refused", err: syscall.ECONNREFUSED, want: true},
		{name: "plain error", err: errors.New("x"), want: false},
		{name: "context canceled", err: context.Canceled, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var netErr net.Error
			if errors.As(tt.err, &netErr) && !tt.want && netErr.Timeout() {
				t.Fatalf("test case invalid: timeout net error should be retryable")
			}

			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
