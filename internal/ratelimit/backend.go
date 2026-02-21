package ratelimit

import (
	"context"
	"time"
)

// LimiterBackend abstracts rate limit enforcement for pluggable backends.
type LimiterBackend interface {
	// Allow checks whether a request identified by key is allowed under the
	// given limit (requests/second) and burst ceiling. It returns whether the
	// request was allowed, the remaining tokens, how long to wait before a retry
	// (zero when allowed), and any error.
	Allow(ctx context.Context, key LimiterKey, limit float64, burst int) (allowed bool, remaining int, retryAfter time.Duration, err error)

	// Close releases resources held by the backend.
	Close() error
}

// RateLimitEntry represents a snapshot of a single rate limiter state.
type RateLimitEntry struct {
	ClientID  string    `json:"client_id"`
	Route     string    `json:"route"`
	Remaining int       `json:"remaining"`
	Limit     float64   `json:"limit"`
	LastUsed  time.Time `json:"last_used"`
}

// LimiterInspector is an optional interface for backends that support state inspection.
type LimiterInspector interface {
	Snapshot() []RateLimitEntry
}
