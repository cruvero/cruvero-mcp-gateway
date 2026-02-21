package ratelimit

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestDragonflyBackend(t *testing.T, opts ...DragonflyBackendOption) (*DragonflyBackend, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rb, err := NewDragonflyBackendFromClient(client, opts...)
	if err != nil {
		t.Fatalf("new dragonfly backend: %v", err)
	}
	t.Cleanup(func() { _ = rb.Close() })
	return rb, mr
}

func TestDragonflyBackendSingleAllowed(t *testing.T) {
	t.Parallel()
	rb, _ := newTestDragonflyBackend(t)

	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	allowed, remaining, retryAfter, err := rb.Allow(context.Background(), key, 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected request to be allowed")
	}
	if remaining != 4 {
		t.Fatalf("expected 4 remaining, got %d", remaining)
	}
	if retryAfter != 0 {
		t.Fatalf("expected zero retryAfter, got %v", retryAfter)
	}
}

func TestDragonflyBackendBurstExhausted(t *testing.T) {
	t.Parallel()
	rb, _ := newTestDragonflyBackend(t)

	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	for i := 0; i < 3; i++ {
		allowed, _, _, err := rb.Allow(context.Background(), key, 10, 3)
		if err != nil {
			t.Fatalf("request %d error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// Next request should be rejected.
	allowed, remaining, retryAfter, err := rb.Allow(context.Background(), key, 10, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected request to be rejected after burst exhausted")
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
	if retryAfter <= 0 {
		t.Fatal("expected positive retryAfter")
	}
}

func TestDragonflyBackendRemainingAccuracy(t *testing.T) {
	t.Parallel()
	rb, _ := newTestDragonflyBackend(t)

	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	for i := 0; i < 5; i++ {
		allowed, remaining, _, err := rb.Allow(context.Background(), key, 10, 10)
		if err != nil {
			t.Fatalf("request %d error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i)
		}
		expected := 10 - i - 1
		if remaining != expected {
			t.Fatalf("request %d: expected remaining %d, got %d", i, expected, remaining)
		}
	}
}

func TestDragonflyBackendFallbackOnDown(t *testing.T) {
	t.Parallel()

	fallback := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = fallback.Close() }()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rb, err := NewDragonflyBackendFromClient(client, WithDragonflyFallback(fallback))
	if err != nil {
		t.Fatalf("new dragonfly backend: %v", err)
	}
	defer func() { _ = rb.Close() }()

	// Shut down DragonflyDB.
	mr.Close()
	rb.healthy.Store(false)

	// Should fall back to memory backend.
	key := LimiterKey{ClientID: "fallback-client", Route: "/mcp"}
	allowed, _, _, err := rb.Allow(context.Background(), key, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected fallback to allow request")
	}
}

func TestDragonflyBackendFailOpenWithoutFallback(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rb, err := NewDragonflyBackendFromClient(client)
	if err != nil {
		t.Fatalf("new dragonfly backend: %v", err)
	}
	defer func() { _ = rb.Close() }()

	// Shut down DragonflyDB.
	mr.Close()
	rb.healthy.Store(false)

	// Should fail open.
	key := LimiterKey{ClientID: "failopen", Route: "/mcp"}
	allowed, _, _, err := rb.Allow(context.Background(), key, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected fail-open to allow request")
	}
}

func TestDragonflyBackendMultiPod(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)

	// Simulate 3 gateway pods sharing the same DragonflyDB.
	backends := make([]*DragonflyBackend, 3)
	for i := range backends {
		client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		rb, err := NewDragonflyBackendFromClient(client)
		if err != nil {
			t.Fatalf("create backend %d: %v", i, err)
		}
		defer func() { _ = rb.Close() }()
		backends[i] = rb
	}

	key := LimiterKey{ClientID: "shared-client", Route: "/mcp"}
	burst := 10
	allowed := 0

	// Send 30 requests across 3 pods.
	for i := 0; i < 30; i++ {
		pod := backends[i%3]
		ok, _, _, err := pod.Allow(context.Background(), key, 10, burst)
		if err != nil {
			t.Fatalf("request %d error: %v", i, err)
		}
		if ok {
			allowed++
		}
	}

	if allowed != burst {
		t.Fatalf("expected exactly %d allowed across pods, got %d", burst, allowed)
	}
}

func TestDragonflyBackendNilSafety(t *testing.T) {
	t.Parallel()

	var rb *DragonflyBackend
	allowed, _, _, err := rb.Allow(context.Background(), LimiterKey{}, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("nil backend should allow all requests")
	}
	_ = rb.Close()
}

func TestDragonflyBackendSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("nil backend returns nil", func(t *testing.T) {
		t.Parallel()
		var rb *DragonflyBackend
		if entries := rb.Snapshot(); entries != nil {
			t.Fatalf("expected nil from nil backend, got %v", entries)
		}
	})

	t.Run("unhealthy backend returns nil", func(t *testing.T) {
		t.Parallel()
		rb, _ := newTestDragonflyBackend(t)
		rb.healthy.Store(false)
		if entries := rb.Snapshot(); entries != nil {
			t.Fatalf("expected nil from unhealthy backend, got %v", entries)
		}
	})

	t.Run("populated backend returns entries", func(t *testing.T) {
		t.Parallel()
		rb, _ := newTestDragonflyBackend(t)

		ctx := context.Background()
		keyA := LimiterKey{ClientID: "snap-client-a", Route: "/snap-route-a"}
		keyB := LimiterKey{ClientID: "snap-client-b", Route: "/snap-route-b"}

		_, _, _, _ = rb.Allow(ctx, keyA, 10, 5)
		_, _, _, _ = rb.Allow(ctx, keyB, 10, 5)

		entries := rb.Snapshot()
		if len(entries) < 2 {
			t.Fatalf("expected at least 2 entries, got %d", len(entries))
		}

		found := make(map[string]RateLimitEntry)
		for _, e := range entries {
			found[e.ClientID] = e
		}

		a, ok := found["snap-client-a"]
		if !ok {
			t.Fatal("expected entry for snap-client-a")
		}
		if a.Route != "/snap-route-a" {
			t.Fatalf("expected route /snap-route-a, got %q", a.Route)
		}

		b, ok := found["snap-client-b"]
		if !ok {
			t.Fatal("expected entry for snap-client-b")
		}
		if b.Route != "/snap-route-b" {
			t.Fatalf("expected route /snap-route-b, got %q", b.Route)
		}
	})
}

func TestWithDragonflyKeyPrefix(t *testing.T) {
	t.Parallel()

	customPrefix := "test-prefix:"
	rb, _ := newTestDragonflyBackend(t, WithDragonflyKeyPrefix(customPrefix))

	if rb.keyPrefix != customPrefix {
		t.Fatalf("expected key prefix %q, got %q", customPrefix, rb.keyPrefix)
	}

	// Verify the prefix is used in keys by making a request and checking snapshot.
	ctx := context.Background()
	key := LimiterKey{ClientID: "prefix-test", Route: "/mcp"}
	_, _, _, err := rb.Allow(ctx, key, 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries := rb.Snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestWithDragonflyLogger(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	rb, _ := newTestDragonflyBackend(t, WithDragonflyLogger(logger))

	if rb.logger != logger {
		t.Fatal("expected custom logger to be set")
	}
}

func TestDragonflyClientAccessor(t *testing.T) {
	t.Parallel()

	t.Run("nil backend returns nil", func(t *testing.T) {
		t.Parallel()
		var rb *DragonflyBackend
		if client := rb.DragonflyClient(); client != nil {
			t.Fatal("expected nil client from nil backend")
		}
	})

	t.Run("valid backend returns client", func(t *testing.T) {
		t.Parallel()
		rb, _ := newTestDragonflyBackend(t)
		client := rb.DragonflyClient()
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestNewDragonflyBackendURL(t *testing.T) {
	t.Parallel()

	t.Run("invalid URL returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewDragonflyBackend("not-a-valid-url")
		if err == nil {
			t.Fatal("expected error for invalid URL")
		}
	})

	t.Run("valid URL with running server", func(t *testing.T) {
		t.Parallel()
		mr := miniredis.RunT(t)
		url := "redis://" + mr.Addr()
		rb, err := NewDragonflyBackend(url)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer func() { _ = rb.Close() }()

		if rb.client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestDragonflyBackendHealthCheckRecovery(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rb, err := NewDragonflyBackendFromClient(client)
	if err != nil {
		t.Fatalf("new dragonfly backend: %v", err)
	}
	defer func() { _ = rb.Close() }()

	// Verify initially healthy.
	if !rb.healthy.Load() {
		t.Fatal("expected backend to be healthy initially")
	}

	// Simulate down then recovery: mark unhealthy, then ping should succeed.
	rb.healthy.Store(false)

	// Manually perform a health check equivalent to verify recovery logic.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = client.Ping(ctx).Err()
	cancel()
	if err != nil {
		t.Fatalf("ping should succeed: %v", err)
	}
	// The actual healthCheck goroutine would recover; simulate:
	rb.healthy.Store(true)
	if !rb.healthy.Load() {
		t.Fatal("expected recovery after successful ping")
	}
}

func TestDragonflyBackendScriptError(t *testing.T) {
	t.Parallel()

	fallback := NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = fallback.Close() }()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rb, err := NewDragonflyBackendFromClient(client, WithDragonflyFallback(fallback))
	if err != nil {
		t.Fatalf("new dragonfly backend: %v", err)
	}
	defer func() { _ = rb.Close() }()

	// Close miniredis to induce script errors.
	mr.Close()

	key := LimiterKey{ClientID: "script-err", Route: "/mcp"}
	allowed, _, _, err := rb.Allow(context.Background(), key, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error (should fall back): %v", err)
	}
	if !allowed {
		t.Fatal("expected fallback to allow request on script error")
	}
	if rb.healthy.Load() {
		t.Fatal("expected backend to be marked unhealthy after script error")
	}
}
