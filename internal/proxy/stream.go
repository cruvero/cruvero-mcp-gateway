package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SSEWriter writes server-sent events for client-facing streaming responses.
type SSEWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

// NewSSEWriter creates an SSE writer from an HTTP response writer.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("sse writer: response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	return &SSEWriter{
		writer:  w,
		flusher: flusher,
	}, nil
}

// WriteEvent writes one SSE event message.
func (s *SSEWriter) WriteEvent(event string, data string) error {
	if s == nil {
		return fmt.Errorf("sse writer: writer is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(event) != "" {
		if _, err := fmt.Fprintf(s.writer, "event: %s\n", event); err != nil {
			return fmt.Errorf("sse writer: write event name: %w", err)
		}
	}

	for _, line := range strings.Split(data, "\n") {
		if _, err := fmt.Fprintf(s.writer, "data: %s\n", line); err != nil {
			return fmt.Errorf("sse writer: write data: %w", err)
		}
	}

	if _, err := fmt.Fprint(s.writer, "\n"); err != nil {
		return fmt.Errorf("sse writer: finalize event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// WriteToolResult serializes and writes a tool result event.
func (s *SSEWriter) WriteToolResult(result *ToolResult) error {
	if result == nil {
		return fmt.Errorf("sse writer: tool result is nil")
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("sse writer: marshal tool result: %w", err)
	}
	return s.WriteEvent("tool_result", string(payload))
}

// Keepalive sends periodic comment heartbeats until context cancellation.
func (s *SSEWriter) Keepalive(ctx context.Context, interval time.Duration) {
	if s == nil || ctx == nil {
		return
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			_, err := fmt.Fprint(s.writer, ": keepalive\n\n")
			if err == nil {
				s.flusher.Flush()
			}
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
