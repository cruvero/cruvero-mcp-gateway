package resilience

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

type mockBackendCaller struct {
	mu      sync.Mutex
	calls   int
	results []backendCallResult
}

type backendCallResult struct {
	result *types.ToolResult
	err    error
}

func (m *mockBackendCaller) CallTool(_ context.Context, _ string, _ map[string]any) (*types.ToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	if len(m.results) == 0 {
		return &types.ToolResult{}, nil
	}

	idx := m.calls - 1
	if idx >= len(m.results) {
		idx = len(m.results) - 1
	}
	return m.results[idx].result, m.results[idx].err
}

func (m *mockBackendCaller) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestResilientClientCallSucceedsFirstAttempt(t *testing.T) {
	t.Parallel()

	backend := &mockBackendCaller{
		results: []backendCallResult{
			{
				result: &types.ToolResult{
					Content: []types.ContentBlock{{Type: "text", Text: "ok"}},
				},
			},
		},
	}
	client := NewResilientClient(
		backend,
		NewCircuitBreaker("svc-a", 2, time.Second),
		RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 1, Jitter: false},
		nil,
	)

	result, err := client.CallTool(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if backend.CallCount() != 1 {
		t.Fatalf("expected one backend call, got %d", backend.CallCount())
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestResilientClientRetriesTransientErrorThenSucceeds(t *testing.T) {
	t.Parallel()

	backend := &mockBackendCaller{
		results: []backendCallResult{
			{err: timeoutErr{}},
			{result: &types.ToolResult{Content: []types.ContentBlock{{Type: "text", Text: "done"}}}},
		},
	}
	client := NewResilientClient(
		backend,
		NewCircuitBreaker("svc-a", 5, time.Second),
		RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, BackoffMultiplier: 2, Jitter: false},
		nil,
	)

	result, err := client.CallTool(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if backend.CallCount() != 2 {
		t.Fatalf("expected two backend calls, got %d", backend.CallCount())
	}
	if result.Content[0].Text != "done" {
		t.Fatalf("expected done, got %#v", result)
	}
}

func TestResilientClientCircuitOpensAfterThreshold(t *testing.T) {
	t.Parallel()

	target := errors.New("backend down")
	backend := &mockBackendCaller{
		results: []backendCallResult{{err: target}},
	}
	breaker := NewCircuitBreaker("svc-a", 2, time.Hour)
	client := NewResilientClient(
		backend,
		breaker,
		RetryConfig{MaxAttempts: 1, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 1, Jitter: false},
		nil,
	)

	_, err := client.CallTool(context.Background(), "echo", nil)
	if !errors.Is(err, target) {
		t.Fatalf("expected backend error on first call, got %v", err)
	}
	_, err = client.CallTool(context.Background(), "echo", nil)
	if !errors.Is(err, target) {
		t.Fatalf("expected backend error on second call, got %v", err)
	}

	if breaker.State() != StateOpen {
		t.Fatalf("expected open circuit, got %s", breaker.State())
	}
	if backend.CallCount() != 2 {
		t.Fatalf("expected two backend calls before circuit opens, got %d", backend.CallCount())
	}
}

func TestResilientClientCircuitOpenReturnsImmediately(t *testing.T) {
	t.Parallel()

	backend := &mockBackendCaller{
		results: []backendCallResult{{result: &types.ToolResult{}}},
	}
	breaker := NewCircuitBreaker("svc-a", 1, time.Hour)
	breaker.state = StateOpen
	breaker.lastFailure = time.Now()
	client := NewResilientClient(
		backend,
		breaker,
		RetryConfig{MaxAttempts: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 1, Jitter: false},
		nil,
	)

	_, err := client.CallTool(context.Background(), "echo", nil)
	if err == nil {
		t.Fatal("expected circuit open error")
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if backend.CallCount() != 0 {
		t.Fatalf("expected zero backend calls, got %d", backend.CallCount())
	}
}

func TestResilientClientHalfOpenRecovery(t *testing.T) {
	t.Parallel()

	backend := &mockBackendCaller{
		results: []backendCallResult{
			{result: &types.ToolResult{Content: []types.ContentBlock{{Type: "text", Text: "recovered"}}}},
		},
	}
	breaker := NewCircuitBreaker("svc-a", 1, 10*time.Millisecond)
	breaker.state = StateOpen
	breaker.lastFailure = time.Now().Add(-time.Second)
	client := NewResilientClient(
		backend,
		breaker,
		RetryConfig{MaxAttempts: 1, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond, BackoffMultiplier: 1, Jitter: false},
		nil,
	)

	result, err := client.CallTool(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("expected recovery success, got %v", err)
	}
	if breaker.State() != StateClosed {
		t.Fatalf("expected closed state after half-open success, got %s", breaker.State())
	}
	if backend.CallCount() != 1 {
		t.Fatalf("expected one probe call, got %d", backend.CallCount())
	}
	if result.Content[0].Text != "recovered" {
		t.Fatalf("unexpected result %#v", result)
	}
}
