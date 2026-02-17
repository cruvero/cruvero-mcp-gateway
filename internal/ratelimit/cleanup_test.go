package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestStartCleanupRemovesIdleEntries(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	store.GetOrCreate(LimiterKey{ClientID: "a", Route: "/mcp"}, nil)

	// Mark as stale to ensure cleanup can remove it once the ticker fires.
	store.mu.Lock()
	store.limiters[LimiterKey{ClientID: "a", Route: "/mcp"}].lastUsed = time.Now().UTC().Add(-time.Hour)
	store.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartCleanup(ctx, store, 10*time.Millisecond, 50*time.Millisecond)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if store.Count() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected cleanup to remove idle entry, count=%d", store.Count())
}

func TestStartCleanupStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	store.GetOrCreate(LimiterKey{ClientID: "a", Route: "/mcp"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	StartCleanup(ctx, store, 10*time.Millisecond, 1*time.Nanosecond)

	time.Sleep(30 * time.Millisecond)
	if store.Count() != 1 {
		t.Fatalf("expected cleanup not to run after canceled context, count=%d", store.Count())
	}
}

func TestStartCleanupPreservesActiveEntries(t *testing.T) {
	t.Parallel()

	store := NewLimiterStore(10, 20)
	store.GetOrCreate(LimiterKey{ClientID: "active", Route: "/mcp"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartCleanup(ctx, store, 10*time.Millisecond, time.Hour)

	time.Sleep(50 * time.Millisecond)
	if store.Count() != 1 {
		t.Fatalf("expected active entry to remain, count=%d", store.Count())
	}
}
