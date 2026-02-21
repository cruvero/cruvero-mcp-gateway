package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryBackendAllowWithinLimit(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer mb.Close()

	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	allowed, remaining, retryAfter, err := mb.Allow(context.Background(), key, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected request to be allowed")
	}
	if remaining < 0 {
		t.Fatalf("expected non-negative remaining, got %d", remaining)
	}
	if retryAfter != 0 {
		t.Fatalf("expected zero retryAfter when allowed, got %v", retryAfter)
	}
}

func TestMemoryBackendRejectOverLimit(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer mb.Close()

	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}

	// Exhaust the single token.
	allowed, _, _, err := mb.Allow(context.Background(), key, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected first request to be allowed")
	}

	// Second request should be rejected.
	allowed, remaining, retryAfter, err := mb.Allow(context.Background(), key, 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected second request to be rejected")
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
	if retryAfter <= 0 {
		t.Fatal("expected positive retryAfter when rejected")
	}
}

func TestMemoryBackendSameKeyShared(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer mb.Close()

	key := LimiterKey{ClientID: "shared", Route: "/mcp"}

	// First request consumes from burst=2.
	allowed, _, _, _ := mb.Allow(context.Background(), key, 1, 2)
	if !allowed {
		t.Fatal("expected first request allowed")
	}

	// Second request also from burst.
	allowed, _, _, _ = mb.Allow(context.Background(), key, 1, 2)
	if !allowed {
		t.Fatal("expected second request allowed")
	}

	// Third should be limited.
	allowed, _, _, _ = mb.Allow(context.Background(), key, 1, 2)
	if allowed {
		t.Fatal("expected third request to be limited")
	}
}

func TestMemoryBackendDifferentKeysIndependent(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer mb.Close()

	keyA := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	keyB := LimiterKey{ClientID: "client-b", Route: "/mcp"}

	allowed, _, _, _ := mb.Allow(context.Background(), keyA, 1, 1)
	if !allowed {
		t.Fatal("expected client-a first request allowed")
	}

	// client-a exhausted, but client-b should still work.
	allowed, _, _, _ = mb.Allow(context.Background(), keyB, 1, 1)
	if !allowed {
		t.Fatal("expected client-b first request allowed")
	}

	allowed, _, _, _ = mb.Allow(context.Background(), keyA, 1, 1)
	if allowed {
		t.Fatal("expected client-a second request to be limited")
	}
}

func TestMemoryBackendCleanupRemovesIdle(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(20*time.Millisecond, 10*time.Millisecond)
	defer mb.Close()

	key := LimiterKey{ClientID: "idle", Route: "/mcp"}
	mb.Allow(context.Background(), key, 10, 20)
	if mb.Count() != 1 {
		t.Fatalf("expected 1 entry, got %d", mb.Count())
	}

	// Wait for cleanup to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if mb.Count() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected cleanup to remove idle entry, count=%d", mb.Count())
}

func TestMemoryBackendCloseStopsGoroutine(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	if err := mb.Close(); err != nil {
		t.Fatalf("unexpected error on close: %v", err)
	}

	// Double close should not panic.
	if err := mb.Close(); err != nil {
		t.Fatalf("unexpected error on double close: %v", err)
	}
}

func TestMemoryBackendSetDefaults(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer mb.Close()

	mb.SetDefaults(50, 100)

	// A request with zero limit/burst should use defaults.
	key := LimiterKey{ClientID: "default-test", Route: "/mcp"}
	allowed, _, _, err := mb.Allow(context.Background(), key, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected request to be allowed with defaults")
	}
}

func TestMemoryBackendNilSafety(t *testing.T) {
	t.Parallel()

	var mb *MemoryBackend
	allowed, _, _, err := mb.Allow(context.Background(), LimiterKey{}, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("nil backend should allow all requests")
	}
	if mb.Count() != 0 {
		t.Fatal("nil backend count should be 0")
	}
	mb.SetDefaults(1, 1)
	_ = mb.Close()
}
