package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestProxyServerSetupMCP(t *testing.T) {
	t.Parallel()

	proxyServer := NewProxyServer(
		registration.NewCapabilityIndex(),
		&config.Config{},
		nil,
		0,
		nil,
	)

	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp: %v", err)
	}
	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp should be idempotent: %v", err)
	}

	if proxyServer.mcpServer == nil {
		t.Fatal("expected underlying mcp server to be initialized")
	}
	if proxyServer.streamableHandler == nil {
		t.Fatal("expected streamable handler to be initialized")
	}
}

func TestProxyServerHandlerInitializeRequest(t *testing.T) {
	t.Parallel()

	proxyServer := NewProxyServer(
		registration.NewCapabilityIndex(),
		&config.Config{},
		nil,
		0,
		nil,
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", proxyServer.Handler())
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-11-25",
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send initialize request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected initialize status 200/202, got %d body=%s", resp.StatusCode, string(responseBody))
	}
}

func TestProxyServerHandlerNilServer(t *testing.T) {
	t.Parallel()

	var proxyServer *ProxyServer
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()

	proxyServer.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestRewriteLegacyToolCallNameRewritesBareTool(t *testing.T) {
	t.Parallel()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"quick_add_task","arguments":{"text":"x"}}}`)
	rewritten, fromName, toName, changed, err := rewriteLegacyToolCallName(body, "mcp-todoist")
	if err != nil {
		t.Fatalf("rewrite legacy tool call: %v", err)
	}
	if !changed {
		t.Fatal("expected legacy call to be rewritten")
	}
	if fromName != "quick_add_task" {
		t.Fatalf("expected fromName quick_add_task, got %q", fromName)
	}
	if toName != "mcp.mcp-todoist.quick_add_task" {
		t.Fatalf("expected federated tool name, got %q", toName)
	}

	var payload map[string]any
	if err := json.Unmarshal(rewritten, &payload); err != nil {
		t.Fatalf("decode rewritten payload: %v", err)
	}
	params, _ := payload["params"].(map[string]any)
	if got, _ := params["name"].(string); got != "mcp.mcp-todoist.quick_add_task" {
		t.Fatalf("expected rewritten params.name, got %q", got)
	}
}

func TestRewriteLegacyToolCallNameNoRewriteForFederatedName(t *testing.T) {
	t.Parallel()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcp.mcp-todoist.quick_add_task","arguments":{"text":"x"}}}`)
	rewritten, _, _, changed, err := rewriteLegacyToolCallName(body, "mcp-todoist")
	if err != nil {
		t.Fatalf("rewrite legacy tool call: %v", err)
	}
	if changed {
		t.Fatal("expected federated name not to be rewritten")
	}
	if string(rewritten) != string(body) {
		t.Fatalf("expected body unchanged, got %s", string(rewritten))
	}
}

func TestRewriteLegacyToolCallNameNoRewriteWithoutServerHint(t *testing.T) {
	t.Parallel()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"quick_add_task","arguments":{"text":"x"}}}`)
	rewritten, _, _, changed, err := rewriteLegacyToolCallName(body, "")
	if err != nil {
		t.Fatalf("rewrite legacy tool call: %v", err)
	}
	if changed {
		t.Fatal("expected no rewrite without server hint")
	}
	if string(rewritten) != string(body) {
		t.Fatalf("expected body unchanged, got %s", string(rewritten))
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "short string under limit",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "string exactly at limit",
			input:  "abcde",
			maxLen: 5,
			want:   "abcde",
		},
		{
			name:   "string over limit",
			input:  "hello world, this is a long string",
			maxLen: 11,
			want:   "hello world",
		},
		{
			name:   "multi-byte unicode truncated at byte boundary",
			input:  "abc" + strings.Repeat("\u00e9", 10), // \u00e9 is 2 bytes in UTF-8
			maxLen: 5,
			want:   "abc" + string([]byte{0xc3, 0xa9}), // 3 + 2 = 5 bytes
		},
		{
			name:   "zero max length",
			input:  "anything",
			maxLen: 0,
			want:   "",
		},
		{
			name:   "max length of one",
			input:  "hello",
			maxLen: 1,
			want:   "h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// mockAuditStore is a test double for store.AuditStore that records logged entries.
type mockAuditStore struct {
	mu      sync.Mutex
	entries []*types.AuditEntry
	logErr  error
	logged  chan struct{}
}

// Log records the audit entry and optionally returns a configured error.
func (m *mockAuditStore) Log(_ context.Context, entry *types.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	if m.logged != nil {
		select {
		case m.logged <- struct{}{}:
		default:
		}
	}
	return m.logErr
}

// Query is a no-op stub required by the AuditStore interface.
func (m *mockAuditStore) Query(_ context.Context, _ types.AuditFilter) ([]types.AuditEntry, error) {
	return nil, nil
}

// getEntries returns a snapshot of logged entries.
func (m *mockAuditStore) getEntries() []*types.AuditEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*types.AuditEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

func TestAuditToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		auditStore      *mockAuditStore
		toolName        string
		responsePreview string
		isError         bool
		expectEntry     bool
	}{
		{
			name:            "nil audit store does not panic",
			auditStore:      nil,
			toolName:        "mcp.test.hello",
			responsePreview: "ok",
			isError:         false,
			expectEntry:     false,
		},
		{
			name: "successful audit log entry",
			auditStore: &mockAuditStore{
				logged: make(chan struct{}, 1),
			},
			toolName:        "mcp.backend.run_query",
			responsePreview: "rows returned: 42",
			isError:         false,
			expectEntry:     true,
		},
		{
			name: "error result is recorded",
			auditStore: &mockAuditStore{
				logged: make(chan struct{}, 1),
			},
			toolName:        "mcp.backend.bad_call",
			responsePreview: "internal error",
			isError:         true,
			expectEntry:     true,
		},
		{
			name: "audit store error is tolerated",
			auditStore: &mockAuditStore{
				logged: make(chan struct{}, 1),
				logErr: fmt.Errorf("database unavailable"),
			},
			toolName:        "mcp.backend.flaky",
			responsePreview: "partial",
			isError:         false,
			expectEntry:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ps := NewProxyServer(
				registration.NewCapabilityIndex(),
				&config.Config{},
				nil,
				0,
				nil,
			)
			if tt.auditStore != nil {
				ps.SetAuditStore(tt.auditStore)
			}

			ps.auditToolCall(tt.toolName, tt.responsePreview, tt.isError)

			if !tt.expectEntry {
				// No store configured; nothing to verify beyond no panic.
				return
			}

			// Wait for the fire-and-forget goroutine to complete its write.
			select {
			case <-tt.auditStore.logged:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for audit log write")
			}

			entries := tt.auditStore.getEntries()
			if len(entries) != 1 {
				t.Fatalf("expected 1 audit entry, got %d", len(entries))
			}

			entry := entries[0]
			if entry.EventType != "tool_call_result" {
				t.Errorf("expected event type %q, got %q", "tool_call_result", entry.EventType)
			}
			if entry.ServerName != tt.toolName {
				t.Errorf("expected server name %q, got %q", tt.toolName, entry.ServerName)
			}
			if entry.Details == nil {
				t.Fatal("expected non-nil details map")
			}
			if got, _ := entry.Details["tool"].(string); got != tt.toolName {
				t.Errorf("expected details.tool %q, got %q", tt.toolName, got)
			}
			if got, _ := entry.Details["response_preview"].(string); got != tt.responsePreview {
				t.Errorf("expected details.response_preview %q, got %q", tt.responsePreview, got)
			}
			if got, _ := entry.Details["is_error"].(bool); got != tt.isError {
				t.Errorf("expected details.is_error %v, got %v", tt.isError, got)
			}
		})
	}
}
