package proxy

import (
	"context"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestSessionAffinityConsistentSelection(t *testing.T) {
	t.Parallel()

	candidates := []types.ServerRecord{
		{ID: "server-1", Name: "alpha", Status: types.StatusActive},
		{ID: "server-2", Name: "bravo", Status: types.StatusActive},
		{ID: "server-3", Name: "charlie", Status: types.StatusActive},
	}

	sa := NewSessionAffinityStrategy(&RoundRobinStrategy{})
	req := &RoutingRequest{SessionID: "session-abc"}

	first, err := sa.Select(context.Background(), candidates, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first == nil {
		t.Fatal("expected non-nil selection")
	}

	for i := 0; i < 1000; i++ {
		got, err := sa.Select(context.Background(), candidates, req)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if got == nil {
			t.Fatalf("iteration %d: expected non-nil selection", i)
		}
		if got.ID != first.ID {
			t.Fatalf("iteration %d: expected %s, got %s", i, first.ID, got.ID)
		}
	}
}

func TestSessionAffinityFallbackOnEmptySession(t *testing.T) {
	t.Parallel()

	candidates := []types.ServerRecord{
		{ID: "server-1", Name: "alpha", Status: types.StatusActive},
		{ID: "server-2", Name: "bravo", Status: types.StatusActive},
	}

	rr := &RoundRobinStrategy{}
	sa := NewSessionAffinityStrategy(rr)
	req := &RoutingRequest{} // No session ID — should fall back.

	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		got, err := sa.Select(context.Background(), candidates, req)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if got == nil {
			t.Fatalf("iteration %d: expected non-nil selection", i)
		}
		counts[got.ID]++
	}

	if counts["server-1"] == 0 || counts["server-2"] == 0 {
		t.Fatalf("expected round-robin distribution, got %v", counts)
	}
}

func TestSessionAffinityDistribution(t *testing.T) {
	t.Parallel()

	candidates := []types.ServerRecord{
		{ID: "server-1", Name: "alpha", Status: types.StatusActive},
		{ID: "server-2", Name: "bravo", Status: types.StatusActive},
		{ID: "server-3", Name: "charlie", Status: types.StatusActive},
	}

	sa := NewSessionAffinityStrategy(&RoundRobinStrategy{})
	counts := map[string]int{}

	for i := 0; i < 300; i++ {
		req := &RoutingRequest{SessionID: testSessionID(i)}
		got, err := sa.Select(context.Background(), candidates, req)
		if err != nil {
			t.Fatalf("session %d: unexpected error: %v", i, err)
		}
		if got == nil {
			t.Fatalf("session %d: expected non-nil selection", i)
		}
		counts[got.ID]++
	}

	for _, id := range []string{"server-1", "server-2", "server-3"} {
		if counts[id] == 0 {
			t.Fatalf("server %s received no traffic; distribution: %v", id, counts)
		}
	}
}

func TestSessionAffinityMinimalRedistribution(t *testing.T) {
	t.Parallel()

	original := []types.ServerRecord{
		{ID: "server-1", Name: "alpha", Status: types.StatusActive},
		{ID: "server-2", Name: "bravo", Status: types.StatusActive},
		{ID: "server-3", Name: "charlie", Status: types.StatusActive},
	}

	sa := NewSessionAffinityStrategy(&RoundRobinStrategy{})
	assignments := map[string]string{}
	sessions := 200
	for i := 0; i < sessions; i++ {
		sid := testSessionID(i)
		req := &RoutingRequest{SessionID: sid}
		got, _ := sa.Select(context.Background(), original, req)
		assignments[sid] = got.ID
	}

	reduced := original[:2]

	moved := 0
	for i := 0; i < sessions; i++ {
		sid := testSessionID(i)
		req := &RoutingRequest{SessionID: sid}
		got, _ := sa.Select(context.Background(), reduced, req)
		prev := assignments[sid]
		if prev == "server-3" {
			continue
		}
		if got.ID != prev {
			moved++
		}
	}

	if moved > 0 {
		t.Fatalf("expected 0 moves among surviving servers, got %d", moved)
	}
}

func TestSessionAffinityEmptyCandidates(t *testing.T) {
	t.Parallel()

	sa := NewSessionAffinityStrategy(&RoundRobinStrategy{})
	req := &RoutingRequest{SessionID: "session-1"}
	got, err := sa.Select(context.Background(), nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for empty candidates")
	}
}

func TestSessionAffinityNilFallback(t *testing.T) {
	t.Parallel()

	sa := NewSessionAffinityStrategy(nil)
	candidates := []types.ServerRecord{
		{ID: "server-1", Name: "alpha", Status: types.StatusActive},
	}
	got, err := sa.Select(context.Background(), candidates, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil selection with nil fallback")
	}
}

func testSessionID(i int) string {
	return "session-" + string(rune('A'+i%26)) + string(rune('0'+i/26%10))
}
