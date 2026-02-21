package ratelimit

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/sliding_window.lua
var slidingWindowScript string

// DragonflyBackendOption configures the DragonflyDB rate limit backend.
type DragonflyBackendOption func(*DragonflyBackend)

// WithDragonflyFallback sets a fallback backend used when DragonflyDB is unavailable.
func WithDragonflyFallback(fallback LimiterBackend) DragonflyBackendOption {
	return func(rb *DragonflyBackend) {
		rb.fallback = fallback
	}
}

// WithDragonflyKeyPrefix sets a prefix for all rate limit keys in DragonflyDB.
func WithDragonflyKeyPrefix(prefix string) DragonflyBackendOption {
	return func(rb *DragonflyBackend) {
		rb.keyPrefix = prefix
	}
}

// WithDragonflyLogger sets a structured logger for the DragonflyDB backend.
func WithDragonflyLogger(logger *slog.Logger) DragonflyBackendOption {
	return func(rb *DragonflyBackend) {
		rb.logger = logger
	}
}

// DragonflyBackend implements distributed rate limiting using DragonflyDB sorted sets
// with a sliding window algorithm.
type DragonflyBackend struct {
	client    *redis.Client
	script    *redis.Script
	keyPrefix string
	fallback  LimiterBackend
	logger    *slog.Logger
	healthy   atomic.Bool

	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewDragonflyBackend creates a DragonflyDB-backed rate limiter from a connection URL.
func NewDragonflyBackend(dragonflyURL string, opts ...DragonflyBackendOption) (*DragonflyBackend, error) {
	options, err := redis.ParseURL(dragonflyURL)
	if err != nil {
		return nil, fmt.Errorf("new dragonfly backend: parse url: %w", err)
	}

	client := redis.NewClient(options)
	return NewDragonflyBackendFromClient(client, opts...)
}

// NewDragonflyBackendFromClient creates a DragonflyDB-backed rate limiter from an existing client.
func NewDragonflyBackendFromClient(client *redis.Client, opts ...DragonflyBackendOption) (*DragonflyBackend, error) {
	rb := &DragonflyBackend{
		client:    client,
		script:    redis.NewScript(slidingWindowScript),
		keyPrefix: "mcpgw:",
		logger:    slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(rb)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("new dragonfly backend: ping: %w", err)
	}
	rb.healthy.Store(true)

	go rb.healthCheck()
	return rb, nil
}

// Allow checks whether a request is allowed under the sliding window rate limit.
func (rb *DragonflyBackend) Allow(ctx context.Context, key LimiterKey, limit float64, burst int) (bool, int, time.Duration, error) {
	if rb == nil {
		return true, 0, 0, nil
	}

	if limit <= 0 {
		limit = defaultRateLimit
	}
	if burst <= 0 {
		burst = defaultRateBurst
	}

	if !rb.healthy.Load() {
		return rb.handleUnavailable(ctx, key, limit, burst)
	}

	normalized := normalizeKey(key)
	dfKey := fmt.Sprintf("%srl:%s:%s", rb.keyPrefix, normalized.ClientID, normalized.Route)

	now := time.Now().UTC()
	nowMicros := now.UnixMicro()
	windowMicros := int64(float64(time.Second.Microseconds()) * float64(burst) / limit)
	ttlMs := int64(math.Ceil(float64(windowMicros) / 1000))
	if ttlMs < 1000 {
		ttlMs = 1000
	}
	member := fmt.Sprintf("%d:%d", nowMicros, now.UnixNano()%1000000)

	result, err := rb.script.Run(ctx, rb.client, []string{dfKey},
		nowMicros, windowMicros, burst, member, ttlMs,
	).Int64Slice()
	if err != nil {
		rb.healthy.Store(false)
		rb.logger.Error("dragonfly rate limit script failed", slog.String("error", err.Error()))
		return rb.handleUnavailable(ctx, key, limit, burst)
	}

	if len(result) != 3 {
		rb.healthy.Store(false)
		return rb.handleUnavailable(ctx, key, limit, burst)
	}

	allowed := result[0] == 1
	remaining := int(result[1])
	retryAfterMs := result[2]

	var retryAfter time.Duration
	if !allowed && retryAfterMs > 0 {
		retryAfter = time.Duration(retryAfterMs) * time.Millisecond
	}

	return allowed, remaining, retryAfter, nil
}

// DragonflyClient returns the underlying DragonflyDB client for sharing with other components.
func (rb *DragonflyBackend) DragonflyClient() *redis.Client {
	if rb == nil {
		return nil
	}
	return rb.client
}

// Close stops the health check goroutine and closes the DragonflyDB connection.
func (rb *DragonflyBackend) Close() error {
	if rb == nil {
		return nil
	}

	rb.mu.Lock()
	select {
	case <-rb.stopCh:
		// already closed
	default:
		close(rb.stopCh)
	}
	rb.mu.Unlock()

	<-rb.doneCh

	if rb.fallback != nil {
		_ = rb.fallback.Close()
	}
	return rb.client.Close()
}

// Snapshot returns a snapshot of all tracked rate limit keys from DragonflyDB.
func (rb *DragonflyBackend) Snapshot() []RateLimitEntry {
	if rb == nil || !rb.healthy.Load() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pattern := rb.keyPrefix + "rl:*"
	var entries []RateLimitEntry

	iter := rb.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// Parse key: prefix + "rl:" + clientID + ":" + route
		trimmed := key[len(rb.keyPrefix+"rl:"):]
		parts := strings.SplitN(trimmed, ":", 2)
		clientID := trimmed
		route := ""
		if len(parts) == 2 {
			clientID = parts[0]
			route = parts[1]
		}

		count := rb.client.ZCard(ctx, key).Val()
		entries = append(entries, RateLimitEntry{
			ClientID:  clientID,
			Route:     route,
			Remaining: int(count),
		})
	}

	return entries
}

func (rb *DragonflyBackend) handleUnavailable(ctx context.Context, key LimiterKey, limit float64, burst int) (bool, int, time.Duration, error) {
	if rb.fallback != nil {
		return rb.fallback.Allow(ctx, key, limit, burst)
	}
	// Fail open: allow the request when DragonflyDB is down and no fallback.
	return true, burst, 0, nil
}

func (rb *DragonflyBackend) healthCheck() {
	defer close(rb.doneCh)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rb.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := rb.client.Ping(ctx).Err()
			cancel()
			if err != nil {
				if rb.healthy.Load() {
					rb.logger.Warn("dragonfly health check failed", slog.String("error", err.Error()))
				}
				rb.healthy.Store(false)
			} else {
				if !rb.healthy.Load() {
					rb.logger.Info("dragonfly health check recovered")
				}
				rb.healthy.Store(true)
			}
		}
	}
}
