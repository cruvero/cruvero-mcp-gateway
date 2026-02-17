package ratelimit

import (
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const (
	defaultRateLimit = 10.0
	defaultRateBurst = 20
)

// LimiterKey identifies one token bucket by caller identity and route.
type LimiterKey struct {
	ClientID string `json:"client_id"`
	Route    string `json:"route"`
}

type entry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// LimiterStore keeps per-(client,route) token buckets.
type LimiterStore struct {
	mu          sync.RWMutex
	limiters    map[LimiterKey]*entry
	defaultRate rate.Limit
	defaultBurst int
}

// NewLimiterStore creates a new in-memory limiter store.
func NewLimiterStore(defaultRate float64, defaultBurst int) *LimiterStore {
	if defaultRate <= 0 {
		defaultRate = defaultRateLimit
	}
	if defaultBurst <= 0 {
		defaultBurst = defaultRateBurst
	}

	return &LimiterStore{
		limiters:    make(map[LimiterKey]*entry),
		defaultRate: rate.Limit(defaultRate),
		defaultBurst: defaultBurst,
	}
}

// GetOrCreate returns an existing limiter or creates one using profile/default limits.
func (s *LimiterStore) GetOrCreate(key LimiterKey, profile *types.PolicyProfile) *rate.Limiter {
	if s == nil {
		return rate.NewLimiter(rate.Limit(defaultRateLimit), defaultRateBurst)
	}

	normalized := normalizeKey(key)
	now := time.Now().UTC()

	s.mu.RLock()
	existing, found := s.limiters[normalized]
	s.mu.RUnlock()

	if found && existing != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if current, ok := s.limiters[normalized]; ok && current != nil {
			current.lastUsed = now
			return current.limiter
		}
	}

	limit := s.defaultRate
	burst := s.defaultBurst
	if profile != nil {
		if profile.RateLimit > 0 {
			limit = rate.Limit(profile.RateLimit)
		}
		if profile.RateBurst > 0 {
			burst = profile.RateBurst
		}
	}
	if limit <= 0 {
		limit = rate.Limit(defaultRateLimit)
	}
	if burst <= 0 {
		burst = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if current, ok := s.limiters[normalized]; ok && current != nil {
		current.lastUsed = now
		return current.limiter
	}

	limiter := rate.NewLimiter(limit, burst)
	s.limiters[normalized] = &entry{
		limiter:  limiter,
		lastUsed: now,
	}
	return limiter
}

// Remove deletes a limiter key from the store.
func (s *LimiterStore) Remove(key LimiterKey) {
	if s == nil {
		return
	}

	normalized := normalizeKey(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.limiters, normalized)
}

// Cleanup removes limiters not used within maxIdle and returns the removal count.
func (s *LimiterStore) Cleanup(maxIdle time.Duration) int {
	if s == nil {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.limiters) == 0 {
		return 0
	}

	if maxIdle <= 0 {
		removed := len(s.limiters)
		clear(s.limiters)
		return removed
	}

	cutoff := time.Now().UTC().Add(-maxIdle)
	removed := 0
	for key, value := range s.limiters {
		if value == nil || value.lastUsed.Before(cutoff) {
			delete(s.limiters, key)
			removed++
		}
	}
	return removed
}

// Count returns the number of active limiters.
func (s *LimiterStore) Count() int {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.limiters)
}

func normalizeKey(key LimiterKey) LimiterKey {
	clientID := strings.TrimSpace(key.ClientID)
	if clientID == "" {
		clientID = "anonymous"
	}

	route := strings.TrimSpace(key.Route)
	if route == "" {
		route = "unknown"
	}

	return LimiterKey{
		ClientID: clientID,
		Route:    route,
	}
}
