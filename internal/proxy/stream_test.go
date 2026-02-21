package proxy

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEWriterWriteEvent(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writer, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("new sse writer: %v", err)
	}

	if err := writer.WriteEvent("update", "line1\nline2"); err != nil {
		t.Fatalf("write event: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: update\n") {
		t.Fatalf("expected event line in body, got %q", body)
	}
	if !strings.Contains(body, "data: line1\n") || !strings.Contains(body, "data: line2\n") {
		t.Fatalf("expected multiline data entries in body, got %q", body)
	}
}

func TestSSEWriterWriteToolResult(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writer, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("new sse writer: %v", err)
	}

	err = writer.WriteToolResult(&ToolResult{
		Content: []ContentBlock{{Type: "text", Text: "ok"}},
		IsError: false,
	})
	if err != nil {
		t.Fatalf("write tool result: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: tool_result\n") {
		t.Fatalf("expected tool_result event, got %q", body)
	}
	if !strings.Contains(body, "\"content\"") {
		t.Fatalf("expected serialized tool result payload, got %q", body)
	}
}

func TestSSEWriterKeepaliveStopsOnCancel(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writer, err := NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("new sse writer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		writer.Keepalive(ctx, 5*time.Millisecond)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("keepalive did not stop on context cancellation")
	}

	if !strings.Contains(rec.Body.String(), ": keepalive\n\n") {
		t.Fatalf("expected keepalive heartbeat in stream, got %q", rec.Body.String())
	}
}
