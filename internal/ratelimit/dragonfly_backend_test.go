package ratelimit

import (
	"context"
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
	defer fallback.Close()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rb, err := NewDragonflyBackendFromClient(client, WithDragonflyFallback(fallback))
	if err != nil {
		t.Fatalf("new dragonfly backend: %v", err)
	}
	defer rb.Close()

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
	defer rb.Close()

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
		defer rb.Close()
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
