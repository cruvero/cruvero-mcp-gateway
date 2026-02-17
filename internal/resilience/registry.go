package resilience

import (
	"sync"
	"time"
)

// BreakerRegistry stores per-backend circuit breakers.
type BreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
}

// NewBreakerRegistry creates an empty breaker registry.
func NewBreakerRegistry() *BreakerRegistry {
	return &BreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// GetOrCreate returns an existing breaker for serverID or creates one.
func (r *BreakerRegistry) GetOrCreate(serverID string, threshold int, timeout time.Duration) *CircuitBreaker {
	r.mu.RLock()
	if breaker, ok := r.breakers[serverID]; ok {
		r.mu.RUnlock()
		return breaker
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if breaker, ok := r.breakers[serverID]; ok {
		return breaker
	}

	breaker := NewCircuitBreaker(serverID, threshold, timeout)
	r.breakers[serverID] = breaker
	return breaker
}

// Remove deletes a breaker from the registry.
func (r *BreakerRegistry) Remove(serverID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, serverID)
}

// ListOpen returns server IDs that currently have an open circuit.
func (r *BreakerRegistry) ListOpen() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	open := make([]string, 0)
	for serverID, breaker := range r.breakers {
		if breaker.State() == StateOpen {
			open = append(open, serverID)
		}
	}
	return open
}
