package llm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

type mockClient struct {
	resp *ChatResponse
	err  error
}

func (m *mockClient) Chat(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return m.resp, m.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFailoverClient_PrimarySucceeds(t *testing.T) {
	t.Parallel()
	primary := &mockClient{resp: &ChatResponse{Content: "hello", Provider: "primary", Model: "m1"}}
	secondary := &mockClient{err: errors.New("should not be called")}

	fc := NewFailoverClient([]ProviderEntry{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, discardLogger())

	resp, err := fc.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("expected content 'hello', got %q", resp.Content)
	}
	if resp.Provider != "primary" {
		t.Fatalf("expected provider 'primary', got %q", resp.Provider)
	}
}

func TestFailoverClient_PrimaryFails_SecondarySucceeds(t *testing.T) {
	t.Parallel()
	primary := &mockClient{err: errors.New("rate limited")}
	secondary := &mockClient{resp: &ChatResponse{Content: "fallback", Provider: "secondary", Model: "m2"}}

	fc := NewFailoverClient([]ProviderEntry{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, discardLogger())

	resp, err := fc.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "fallback" {
		t.Fatalf("expected content 'fallback', got %q", resp.Content)
	}
	if resp.Provider != "secondary" {
		t.Fatalf("expected provider 'secondary', got %q", resp.Provider)
	}
}

func TestFailoverClient_AllFail(t *testing.T) {
	t.Parallel()
	err1 := errors.New("primary error")
	err2 := errors.New("secondary error")

	fc := NewFailoverClient([]ProviderEntry{
		{Name: "p1", Client: &mockClient{err: err1}},
		{Name: "p2", Client: &mockClient{err: err2}},
	}, discardLogger())

	_, err := fc.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if !strings.Contains(err.Error(), "all 2 LLM providers failed") {
		t.Fatalf("expected 'all 2 LLM providers failed' in error, got: %v", err)
	}
	if !errors.Is(err, err2) {
		t.Fatalf("expected last error to be wrapped, got: %v", err)
	}
}

func TestFailoverClient_SingleProvider(t *testing.T) {
	t.Parallel()
	only := &mockClient{resp: &ChatResponse{Content: "ok", Provider: "only", Model: "m"}}

	fc := NewFailoverClient([]ProviderEntry{
		{Name: "only", Client: only},
	}, discardLogger())

	resp, err := fc.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
}

func TestFailoverClient_EmptyProviders(t *testing.T) {
	t.Parallel()
	fc := NewFailoverClient(nil, discardLogger())

	_, err := fc.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("expected error with no providers")
	}
	if !strings.Contains(err.Error(), "no LLM providers configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewFailoverClient_NilLogger(t *testing.T) {
	t.Parallel()
	fc := NewFailoverClient([]ProviderEntry{
		{Name: "test", Client: &mockClient{resp: &ChatResponse{Content: "ok", Provider: "test", Model: "m"}}},
	}, nil)

	resp, err := fc.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected 'ok', got %q", resp.Content)
	}
}
