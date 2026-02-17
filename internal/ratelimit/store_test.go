package ratelimit

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestLimiterStoreGetOrCreateCreatesNewLimiter(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	profile := &types.PolicyProfile{RateLimit: 50, RateBurst: 100}

	limiter := store.GetOrCreate(key, profile)
	if limiter == nil {
		t.Fatal("expected limiter")
	}
	if got := float64(limiter.Limit()); got != 50 {
		t.Fatalf("expected rate 50, got %v", got)
	}
	if got := limiter.Burst(); got != 100 {
		t.Fatalf("expected burst 100, got %d", got)
	}
}

func TestLimiterStoreGetOrCreateReturnsExistingForSameKey(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}

	first := store.GetOrCreate(key, &types.PolicyProfile{RateLimit: 10, RateBurst: 20})
	second := store.GetOrCreate(key, &types.PolicyProfile{RateLimit: 100, RateBurst: 100})
	if first != second {
		t.Fatal("expected same limiter for same key")
	}
}

func TestLimiterStoreDifferentKeysGetIndependentLimiters(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)

	limiterA := store.GetOrCreate(LimiterKey{ClientID: "a", Route: "/mcp"}, nil)
	limiterB := store.GetOrCreate(LimiterKey{ClientID: "b", Route: "/mcp"}, nil)
	if limiterA == limiterB {
		t.Fatal("expected distinct limiters for different keys")
	}
}

func TestLimiterStoreRemoveDeletesEntry(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	store.GetOrCreate(key, nil)
	if store.Count() != 1 {
		t.Fatalf("expected count 1, got %d", store.Count())
	}

	store.Remove(key)
	if store.Count() != 0 {
		t.Fatalf("expected count 0 after remove, got %d", store.Count())
	}
}

func TestLimiterStoreCleanupRemovesIdlePreservesActive(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	idleKey := LimiterKey{ClientID: "idle", Route: "/mcp"}
	activeKey := LimiterKey{ClientID: "active", Route: "/mcp"}

	store.GetOrCreate(idleKey, nil)
	store.GetOrCreate(activeKey, nil)

	store.mu.Lock()
	store.limiters[idleKey].lastUsed = time.Now().UTC().Add(-10 * time.Minute)
	store.limiters[activeKey].lastUsed = time.Now().UTC()
	store.mu.Unlock()

	removed := store.Cleanup(2 * time.Minute)
	if removed != 1 {
		t.Fatalf("expected 1 removed limiter, got %d", removed)
	}
	if store.Count() != 1 {
		t.Fatalf("expected 1 limiter remaining, got %d", store.Count())
	}
	if _, ok := store.limiters[activeKey]; !ok {
		t.Fatal("expected active limiter to remain")
	}
}

func TestLimiterStoreCountReflectsCurrentState(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	if got := store.Count(); got != 0 {
		t.Fatalf("expected initial count 0, got %d", got)
	}

	store.GetOrCreate(LimiterKey{ClientID: "a", Route: "/mcp"}, nil)
	store.GetOrCreate(LimiterKey{ClientID: "b", Route: "/mcp"}, nil)
	if got := store.Count(); got != 2 {
		t.Fatalf("expected count 2, got %d", got)
	}
}

func TestLimiterStoreConcurrentAccessSafety(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	key := LimiterKey{ClientID: "concurrent", Route: "/mcp"}
	const workers = 64

	results := make([]*rate.Limiter, workers)
	wg := sync.WaitGroup{}
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = store.GetOrCreate(key, &types.PolicyProfile{RateLimit: 20, RateBurst: 30})
		}(i)
	}
	wg.Wait()

	first := results[0]
	if first == nil {
		t.Fatal("expected limiter from concurrent call")
	}
	for i := 1; i < workers; i++ {
		if results[i] != first {
			t.Fatalf("expected same limiter pointer at index %d", i)
		}
	}
}

func TestLimiterStoreAppliesDefaultsWhenProfileMissingOrZero(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(7, 9)
	keyA := LimiterKey{ClientID: "a", Route: "/x"}
	keyB := LimiterKey{ClientID: "b", Route: "/x"}

	limiterA := store.GetOrCreate(keyA, nil)
	limiterB := store.GetOrCreate(keyB, &types.PolicyProfile{RateLimit: 0, RateBurst: 0})

	if limiterA.Burst() != 9 || limiterB.Burst() != 9 {
		t.Fatalf("expected default burst 9, got %d and %d", limiterA.Burst(), limiterB.Burst())
	}
	if float64(limiterA.Limit()) != 7 || float64(limiterB.Limit()) != 7 {
		t.Fatalf("expected default rate 7, got %v and %v", limiterA.Limit(), limiterB.Limit())
	}
}
