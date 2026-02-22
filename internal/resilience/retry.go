package resilience

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
)

const (
	defaultInitialBackoff  = 100 * time.Millisecond
	defaultMaxBackoff      = 2 * time.Second
	defaultBackoffMultiple = 2.0
	defaultJitterEnabled   = true
)

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxAttempts       int           `json:"max_attempts"`
	InitialBackoff    time.Duration `json:"initial_backoff"`
	MaxBackoff        time.Duration `json:"max_backoff"`
	BackoffMultiplier float64       `json:"backoff_multiplier"`
	Jitter            bool          `json:"jitter"`
}

// DefaultRetryConfig returns retry configuration with env-backed defaults.
func DefaultRetryConfig() RetryConfig {
	cfg := RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    defaultInitialBackoff,
		MaxBackoff:        defaultMaxBackoff,
		BackoffMultiplier: defaultBackoffMultiple,
		Jitter:            defaultJitterEnabled,
	}

	loaded, err := config.Load()
	if err != nil {
		return cfg
	}

	cfg.MaxAttempts = loaded.RetryMax
	return normalizeRetryConfig(cfg)
}

// Retry executes fn until success, non-retryable failure, context cancellation, or attempt exhaustion.
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	if fn == nil {
		return fmt.Errorf("retry: function is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cfg = normalizeRetryConfig(cfg)
	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("retry attempt %d canceled: %w", attempt, err)
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		if !IsRetryable(err) {
			return fmt.Errorf("retry aborted after %d attempt(s): %w", attempt, err)
		}
		if attempt == cfg.MaxAttempts {
			break
		}

		if err := waitBackoff(ctx, cfg, attempt); err != nil {
			return err
		}
	}

	if lastErr == nil {
		return fmt.Errorf("retry exhausted: no attempts executed")
	}
	return fmt.Errorf("retry failed after %d attempt(s): %w", cfg.MaxAttempts, lastErr)
}

// waitBackoff sleeps for the calculated backoff duration or returns early on context cancellation.
func waitBackoff(ctx context.Context, cfg RetryConfig, attempt int) error {
	delay := calculateBackoff(cfg, attempt)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
		return fmt.Errorf("retry canceled after %d attempt(s): %w", attempt, ctx.Err())
	case <-timer.C:
		return nil
	}
}

// IsRetryable returns true when the error indicates a transient failure.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var statusErr interface{ StatusCode() int }
	if errors.As(err, &statusErr) {
		return isRetryableStatus(statusErr.StatusCode())
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return true
		}
	}

	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return true
	}

	return false
}

func normalizeRetryConfig(cfg RetryConfig) RetryConfig {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = defaultInitialBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultMaxBackoff
	}
	if cfg.BackoffMultiplier < 1 {
		cfg.BackoffMultiplier = defaultBackoffMultiple
	}
	return cfg
}

func calculateBackoff(cfg RetryConfig, attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	delay := float64(cfg.InitialBackoff) * math.Pow(cfg.BackoffMultiplier, float64(attempt-1))
	if cfg.MaxBackoff > 0 && delay > float64(cfg.MaxBackoff) {
		delay = float64(cfg.MaxBackoff)
	}

	out := time.Duration(delay)
	if cfg.Jitter {
		return addJitter(out)
	}
	return out
}

func addJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}

	max := big.NewInt(10_001)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return base
	}
	fraction := float64(n.Int64()) / 10000.0
	factor := 0.75 + (0.5 * fraction)
	return time.Duration(float64(base) * factor)
}

func isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusTooEarly,         // 425
		http.StatusTooManyRequests,  // 429
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
