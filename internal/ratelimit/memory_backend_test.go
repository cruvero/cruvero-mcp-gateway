package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryBackendAllowWithinLimit(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = mb.Close() }()

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
	defer func() { _ = mb.Close() }()

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
	defer func() { _ = mb.Close() }()

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
	defer func() { _ = mb.Close() }()

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
	defer func() { _ = mb.Close() }()

	key := LimiterKey{ClientID: "idle", Route: "/mcp"}
	_, _, _, _ = mb.Allow(context.Background(), key, 10, 20)
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
	defer func() { _ = mb.Close() }()

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

func TestMemoryBackendSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("nil backend returns nil", func(t *testing.T) {
		t.Parallel()
		var mb *MemoryBackend
		if entries := mb.Snapshot(); entries != nil {
			t.Fatalf("expected nil snapshot from nil backend, got %v", entries)
		}
	})

	t.Run("empty backend returns empty slice", func(t *testing.T) {
		t.Parallel()
		mb := NewMemoryBackend(time.Minute, 5*time.Minute)
		defer func() { _ = mb.Close() }()

		entries := mb.Snapshot()
		if len(entries) != 0 {
			t.Fatalf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("populated backend returns entries", func(t *testing.T) {
		t.Parallel()
		mb := NewMemoryBackend(time.Minute, 5*time.Minute)
		defer func() { _ = mb.Close() }()

		ctx := context.Background()
		keyA := LimiterKey{ClientID: "client-snap-a", Route: "/route-a"}
		keyB := LimiterKey{ClientID: "client-snap-b", Route: "/route-b"}

		_, _, _, _ = mb.Allow(ctx, keyA, 10, 20)
		_, _, _, _ = mb.Allow(ctx, keyB, 5, 10)

		entries := mb.Snapshot()
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}

		found := make(map[string]RateLimitEntry)
		for _, e := range entries {
			found[e.ClientID] = e
		}

		a, ok := found["client-snap-a"]
		if !ok {
			t.Fatal("expected entry for client-snap-a")
		}
		if a.Route != "/route-a" {
			t.Fatalf("expected route /route-a, got %q", a.Route)
		}
		if a.Limit != 10 {
			t.Fatalf("expected limit 10, got %v", a.Limit)
		}
		if a.Remaining < 0 {
			t.Fatalf("expected non-negative remaining, got %d", a.Remaining)
		}
		if a.LastUsed.IsZero() {
			t.Fatal("expected non-zero LastUsed")
		}

		b, ok := found["client-snap-b"]
		if !ok {
			t.Fatal("expected entry for client-snap-b")
		}
		if b.Route != "/route-b" {
			t.Fatalf("expected route /route-b, got %q", b.Route)
		}
		if b.Limit != 5 {
			t.Fatalf("expected limit 5, got %v", b.Limit)
		}
	})

	t.Run("key without route separator", func(t *testing.T) {
		t.Parallel()
		mb := NewMemoryBackend(time.Minute, 5*time.Minute)
		defer func() { _ = mb.Close() }()

		// Use a key with only ClientID, empty Route to produce a key without ":"
		keyNoRoute := LimiterKey{ClientID: "solo-client", Route: ""}
		_, _, _, _ = mb.Allow(context.Background(), keyNoRoute, 10, 20)

		entries := mb.Snapshot()
		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}
		// When route is empty, the normalizeKey produces "solo-client:" which splits into 2 parts.
		// The snapshot should still return a valid entry.
		e := entries[0]
		if e.ClientID == "" {
			t.Fatal("expected non-empty ClientID in snapshot entry")
		}
	})
}

func TestMemoryBackendDefaultsWhenZero(t *testing.T) {
	t.Parallel()

	mb := NewMemoryBackend(0, 0)
	defer func() { _ = mb.Close() }()

	// Should use default cleanupInterval and maxIdle without panicking.
	key := LimiterKey{ClientID: "defaults", Route: "/mcp"}
	allowed, _, _, err := mb.Allow(context.Background(), key, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected request to be allowed")
	}
}
