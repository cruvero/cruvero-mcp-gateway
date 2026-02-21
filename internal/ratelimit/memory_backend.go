package ratelimit

import (
	"context"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultMemoryCleanupInterval = time.Minute
	defaultMemoryMaxIdle         = 5 * time.Minute
)

type memoryEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// MemoryBackend is an in-process token-bucket rate limiter backend.
type MemoryBackend struct {
	mu              sync.RWMutex
	limiters        map[string]*memoryEntry
	defaultRate     float64
	defaultBurst    int
	cleanupInterval time.Duration
	maxIdle         time.Duration
	stopCh          chan struct{}
	doneCh          chan struct{}
}

// NewMemoryBackend creates a memory-based rate limit backend with a background
// cleanup goroutine that removes idle entries.
func NewMemoryBackend(cleanupInterval, maxIdle time.Duration) *MemoryBackend {
	if cleanupInterval <= 0 {
		cleanupInterval = defaultMemoryCleanupInterval
	}
	if maxIdle <= 0 {
		maxIdle = defaultMemoryMaxIdle
	}

	mb := &MemoryBackend{
		limiters:        make(map[string]*memoryEntry),
		defaultRate:     defaultRateLimit,
		defaultBurst:    defaultRateBurst,
		cleanupInterval: cleanupInterval,
		maxIdle:         maxIdle,
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
	go mb.cleanupLoop()
	return mb
}

// Allow checks the local token bucket for the given key.
func (mb *MemoryBackend) Allow(_ context.Context, key LimiterKey, limit float64, burst int) (bool, int, time.Duration, error) {
	if mb == nil {
		return true, 0, 0, nil
	}

	if limit <= 0 {
		limit = mb.defaultRate
	}
	if burst <= 0 {
		burst = mb.defaultBurst
	}

	normalized := normalizeKey(key)
	mapKey := normalized.String()
	now := time.Now().UTC()

	mb.mu.RLock()
	existing, found := mb.limiters[mapKey]
	mb.mu.RUnlock()

	if found && existing != nil {
		mb.mu.Lock()
		if current, ok := mb.limiters[mapKey]; ok && current != nil {
			current.lastUsed = now
			mb.mu.Unlock()
			return mb.checkLimiter(current.limiter, limit)
		}
		mb.mu.Unlock()
	}

	limiter := rate.NewLimiter(rate.Limit(limit), burst)
	mb.mu.Lock()
	if current, ok := mb.limiters[mapKey]; ok && current != nil {
		current.lastUsed = now
		mb.mu.Unlock()
		return mb.checkLimiter(current.limiter, limit)
	}
	mb.limiters[mapKey] = &memoryEntry{limiter: limiter, lastUsed: now}
	mb.mu.Unlock()

	return mb.checkLimiter(limiter, limit)
}

func (mb *MemoryBackend) checkLimiter(limiter *rate.Limiter, limit float64) (bool, int, time.Duration, error) {
	allowed := limiter.Allow()
	remaining := int(math.Floor(limiter.Tokens()))
	if remaining < 0 {
		remaining = 0
	}

	var retryAfter time.Duration
	if !allowed {
		needed := 1 - limiter.Tokens()
		if needed <= 0 {
			needed = 1
		}
		if limit > 0 {
			retryAfter = time.Duration(float64(time.Second) * needed / limit)
			if retryAfter <= 0 {
				retryAfter = time.Second
			}
		} else {
			retryAfter = time.Second
		}
	}

	return allowed, remaining, retryAfter, nil
}

// SetDefaults updates default rate and burst values for newly created buckets.
func (mb *MemoryBackend) SetDefaults(defaultRate float64, defaultBurst int) {
	if mb == nil {
		return
	}

	r := defaultRate
	if r <= 0 {
		r = defaultRateLimit
	}
	b := defaultBurst
	if b <= 0 {
		b = defaultRateBurst
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.defaultRate = r
	mb.defaultBurst = b
}

// Count returns the number of active limiters.
func (mb *MemoryBackend) Count() int {
	if mb == nil {
		return 0
	}

	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return len(mb.limiters)
}

// Close stops the background cleanup goroutine and releases resources.
func (mb *MemoryBackend) Close() error {
	if mb == nil {
		return nil
	}

	select {
	case <-mb.stopCh:
		// already closed
	default:
		close(mb.stopCh)
	}
	<-mb.doneCh
	return nil
}

func (mb *MemoryBackend) cleanupLoop() {
	defer close(mb.doneCh)

	ticker := time.NewTicker(mb.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mb.stopCh:
			return
		case <-ticker.C:
			mb.cleanup()
		}
	}
}

func (mb *MemoryBackend) cleanup() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	cutoff := time.Now().UTC().Add(-mb.maxIdle)
	for key, entry := range mb.limiters {
		if entry == nil || entry.lastUsed.Before(cutoff) {
			delete(mb.limiters, key)
		}
	}
}
