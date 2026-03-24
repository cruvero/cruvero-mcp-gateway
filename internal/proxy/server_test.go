package proxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto/tls"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

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
	if toName != "todoist.quick_add_task" {
		t.Fatalf("expected federated tool name, got %q", toName)
	}

	var payload map[string]any
	if err := json.Unmarshal(rewritten, &payload); err != nil {
		t.Fatalf("decode rewritten payload: %v", err)
	}
	params, _ := payload["params"].(map[string]any)
	if got, _ := params["name"].(string); got != "todoist.quick_add_task" {
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

func TestRewriteLegacyToolCallNameNoRewriteForNewFormatName(t *testing.T) {
	t.Parallel()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"todoist.quick_add_task","arguments":{"text":"x"}}}`)
	rewritten, _, _, changed, err := rewriteLegacyToolCallName(body, "mcp-todoist")
	if err != nil {
		t.Fatalf("rewrite legacy tool call: %v", err)
	}
	if changed {
		t.Fatal("expected new-format federated name not to be rewritten")
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
func (m *mockAuditStore) Count(_ context.Context, _ types.AuditFilter) (int, error) {
	return 0, nil
}

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
			toolName:        "test.hello",
			responsePreview: "ok",
			isError:         false,
			expectEntry:     false,
		},
		{
			name: "successful audit log entry",
			auditStore: &mockAuditStore{
				logged: make(chan struct{}, 1),
			},
			toolName:        "backend.run_query",
			responsePreview: "rows returned: 42",
			isError:         false,
			expectEntry:     true,
		},
		{
			name: "error result is recorded",
			auditStore: &mockAuditStore{
				logged: make(chan struct{}, 1),
			},
			toolName:        "backend.bad_call",
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
			toolName:        "backend.flaky",
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

func TestInvalidateToolCache(t *testing.T) {
	t.Parallel()

	t.Run("nil ProxyServer does not panic", func(t *testing.T) {
		t.Parallel()
		var ps *ProxyServer
		ps.InvalidateToolCache()
	})

	t.Run("nil toolCache does not panic", func(t *testing.T) {
		t.Parallel()
		ps := &ProxyServer{}
		ps.InvalidateToolCache()
	})

	t.Run("clears all cached entries", func(t *testing.T) {
		t.Parallel()
		cache := NewToolCache(time.Minute)
		cache.Set("srv.tool1", ToolDefinition{Name: "srv.tool1"}, "srv", "srv")
		cache.Set("srv.tool2", ToolDefinition{Name: "srv.tool2"}, "srv", "srv")

		ps := NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)
		ps.toolCache = cache

		if _, ok := cache.Get("srv.tool1"); !ok {
			t.Fatal("expected tool1 in cache before invalidation")
		}

		ps.InvalidateToolCache()

		if _, ok := cache.Get("srv.tool1"); ok {
			t.Error("expected tool1 evicted after full invalidation")
		}
		if _, ok := cache.Get("srv.tool2"); ok {
			t.Error("expected tool2 evicted after full invalidation")
		}
	})
}

func TestInvalidateToolCacheForServer(t *testing.T) {
	t.Parallel()

	t.Run("nil ProxyServer does not panic", func(t *testing.T) {
		t.Parallel()
		var ps *ProxyServer
		ps.InvalidateToolCacheForServer("srv-1")
	})

	t.Run("nil toolCache does not panic", func(t *testing.T) {
		t.Parallel()
		ps := &ProxyServer{}
		ps.InvalidateToolCacheForServer("srv-1")
	})

	t.Run("removes only entries for target server", func(t *testing.T) {
		t.Parallel()
		cache := NewToolCache(time.Minute)
		cache.Set("srv1.tool", ToolDefinition{Name: "srv1.tool"}, "srv-1", "srv-1")
		cache.Set("srv2.tool", ToolDefinition{Name: "srv2.tool"}, "srv-2", "srv-2")

		ps := NewProxyServer(
			registration.NewCapabilityIndex(),
			&config.Config{},
			nil, 0, nil,
		)
		ps.toolCache = cache

		ps.InvalidateToolCacheForServer("srv-1")

		if _, ok := cache.Get("srv1.tool"); ok {
			t.Error("expected srv-1 tool evicted")
		}
		if _, ok := cache.Get("srv2.tool"); !ok {
			t.Error("expected srv-2 tool to remain")
		}
	})
}

func TestSyncMCPToolsProgressiveDiscoveryEnabled(t *testing.T) {
	t.Parallel()

	// Set up a real backend MCP server with multiple tools.
	mcpSrv := mcpserver.NewMCPServer("backend-discovery", "1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("create_issue", mcp.WithDescription("Create a new issue in a repository. Supports labels and assignees."), mcp.WithString("title", mcp.Required())),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("created"), nil
		},
	)
	mcpSrv.AddTool(
		mcp.NewTool("list_issues", mcp.WithDescription("List issues."), mcp.WithString("repo")),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("listed"), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	backendSrv := httptest.NewTLSServer(mux)
	defer backendSrv.Close()

	record := recordFromServerURL(t, backendSrv.URL)
	record.ID = "backend-disc-1"
	record.Name = "github"
	record.Status = types.StatusActive

	rootPool := x509.NewCertPool()
	rootPool.AddCert(backendSrv.Certificate())
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootPool}

	capIdx := registration.NewCapabilityIndex()
	record.Capabilities = types.Capability{Tools: []string{"create_issue", "list_issues"}}
	capIdx.Add(record)

	proxyServer := NewProxyServer(
		capIdx,
		&config.Config{ProgressiveDiscovery: true},
		tlsCfg,
		2*time.Second,
		nil,
	)
	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp: %v", err)
	}

	if err := proxyServer.syncMCPTools(context.Background()); err != nil {
		t.Fatalf("sync tools: %v", err)
	}

	if proxyServer.discoveryIndex == nil {
		t.Fatal("expected discoveryIndex to be non-nil when progressive discovery enabled")
	}

	// Backend tools, meta-tools, and request_access must all be registered.
	allTools := proxyServer.mcpServer.ListTools()

	expectedTools := map[string]bool{
		"github.create_issue":  true,
		"github.list_issues":   true,
		"search_tools":         true,
		"get_tool_schema":      true,
		requestAccessToolName:  true,
	}
	if len(allTools) != len(expectedTools) {
		t.Errorf("expected %d registered tools, got %d", len(expectedTools), len(allTools))
	}
	for name := range allTools {
		if !expectedTools[name] {
			t.Errorf("unexpected tool %q in registry", name)
		}
	}

	// Discovery index must still contain backend tools.
	results, total := proxyServer.discoveryIndex.Search("issue", "", 0, 10)
	if total == 0 || len(results) == 0 {
		t.Error("expected discovery index to contain backend tools")
	}
}

func TestSyncMCPToolsProgressiveDiscoveryDisabled(t *testing.T) {
	t.Parallel()

	mcpSrv := mcpserver.NewMCPServer("backend-no-disc", "1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("echo", mcp.WithDescription("echo input"), mcp.WithString("message", mcp.Required())),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			msg, _ := req.RequireString("message")
			return mcp.NewToolResultText(msg), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	backendSrv := httptest.NewTLSServer(mux)
	defer backendSrv.Close()

	record := recordFromServerURL(t, backendSrv.URL)
	record.ID = "backend-no-disc-1"
	record.Name = "echo-server"
	record.Status = types.StatusActive

	rootPool := x509.NewCertPool()
	rootPool.AddCert(backendSrv.Certificate())
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootPool}

	capIdx := registration.NewCapabilityIndex()
	record.Capabilities = types.Capability{Tools: []string{"echo"}}
	capIdx.Add(record)

	proxyServer := NewProxyServer(
		capIdx,
		&config.Config{ProgressiveDiscovery: false},
		tlsCfg,
		2*time.Second,
		nil,
	)
	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp: %v", err)
	}

	if err := proxyServer.syncMCPTools(context.Background()); err != nil {
		t.Fatalf("sync tools: %v", err)
	}

	if proxyServer.discoveryIndex != nil {
		t.Error("expected discoveryIndex to be nil when progressive discovery disabled")
	}

	allTools := proxyServer.mcpServer.ListTools()
	for name := range allTools {
		if name == "search_tools" || name == "get_tool_schema" {
			t.Errorf("meta-tool %q should not be registered when progressive discovery disabled", name)
		}
	}
}

// mockSession implements server.SessionWithTools for unit testing session tool
// activation without requiring a real HTTP transport.
type mockSession struct {
	id          string
	initialized bool
	notifCh     chan mcp.JSONRPCNotification
	mu          sync.Mutex
	tools       map[string]mcpserver.ServerTool
}

func newMockSession(id string) *mockSession {
	return &mockSession{
		id:          id,
		initialized: true,
		notifCh:     make(chan mcp.JSONRPCNotification, 16),
		tools:       make(map[string]mcpserver.ServerTool),
	}
}

func (s *mockSession) Initialize()                                        { s.initialized = true }
func (s *mockSession) Initialized() bool                                  { return s.initialized }
func (s *mockSession) NotificationChannel() chan<- mcp.JSONRPCNotification { return s.notifCh }
func (s *mockSession) SessionID() string                                  { return s.id }
func (s *mockSession) GetSessionTools() map[string]mcpserver.ServerTool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]mcpserver.ServerTool, len(s.tools))
	maps.Copy(out, s.tools)
	return out
}
func (s *mockSession) SetSessionTools(tools map[string]mcpserver.ServerTool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = tools
}

func TestBuildSearchToolsMetaHandlers(t *testing.T) {
	t.Parallel()

	mcpSrv := mcpserver.NewMCPServer("backend-meta", "1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("create_issue", mcp.WithDescription("Create a new issue in a repository."), mcp.WithString("title", mcp.Required())),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("created"), nil
		},
	)
	mcpSrv.AddTool(
		mcp.NewTool("send_message", mcp.WithDescription("Send a Slack message."), mcp.WithString("text", mcp.Required())),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("sent"), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	backendSrv := httptest.NewTLSServer(mux)
	defer backendSrv.Close()

	record := recordFromServerURL(t, backendSrv.URL)
	record.ID = "backend-meta-1"
	record.Name = "testsvr"
	record.Status = types.StatusActive

	rootPool := x509.NewCertPool()
	rootPool.AddCert(backendSrv.Certificate())
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootPool}

	capIdx := registration.NewCapabilityIndex()
	record.Capabilities = types.Capability{Tools: []string{"create_issue", "send_message"}}
	capIdx.Add(record)

	proxyServer := NewProxyServer(
		capIdx,
		&config.Config{ProgressiveDiscovery: true},
		tlsCfg,
		2*time.Second,
		nil,
	)
	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp: %v", err)
	}
	if err := proxyServer.syncMCPTools(context.Background()); err != nil {
		t.Fatalf("sync tools: %v", err)
	}

	t.Run("search_tools handler returns results", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["search_tools"]
		if st == nil {
			t.Fatal("search_tools not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "search_tools",
				Arguments: map[string]any{"query": "issue"},
			},
		})
		if err != nil {
			t.Fatalf("search_tools handler: %v", err)
		}
		if result.IsError {
			t.Fatal("expected non-error result")
		}
		if len(result.Content) == 0 {
			t.Fatal("expected content in result")
		}

		var resp struct {
			Tools        []json.RawMessage `json:"tools"`
			TotalMatches int               `json:"total_matches"`
			Showing      int               `json:"showing"`
		}
		text := result.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.TotalMatches == 0 {
			t.Error("expected at least one match for 'issue'")
		}
		if resp.Showing != len(resp.Tools) {
			t.Errorf("showing=%d does not match tools length=%d", resp.Showing, len(resp.Tools))
		}
	})

	t.Run("search_tools handler with category and limit", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["search_tools"]
		if st == nil {
			t.Fatal("search_tools not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "search_tools",
				Arguments: map[string]any{"query": "message", "category": "testsvr", "limit": float64(1)},
			},
		})
		if err != nil {
			t.Fatalf("search_tools handler: %v", err)
		}

		var resp struct {
			TotalMatches int `json:"total_matches"`
			Showing      int `json:"showing"`
		}
		text := result.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Showing > 1 {
			t.Errorf("expected at most 1 result with limit=1, got %d", resp.Showing)
		}
	})

	t.Run("get_tool_schema handler returns full definitions", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["get_tool_schema"]
		if st == nil {
			t.Fatal("get_tool_schema not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"testsvr.create_issue"}},
			},
		})
		if err != nil {
			t.Fatalf("get_tool_schema handler: %v", err)
		}
		if result.IsError {
			t.Fatal("expected non-error result")
		}

		var tools []ToolDefinition
		text := result.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &tools); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		if tools[0].Name != "testsvr.create_issue" {
			t.Errorf("expected tool name testsvr.create_issue, got %q", tools[0].Name)
		}
		if string(tools[0].InputSchema) == "{}" {
			t.Error("expected full input schema, got stripped version")
		}
	})

	t.Run("search_tools handler missing query", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["search_tools"]
		if st == nil {
			t.Fatal("search_tools not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "search_tools",
				Arguments: map[string]any{},
			},
		})
		if err != nil {
			t.Fatalf("search_tools handler: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result when query is missing")
		}
	})

	t.Run("get_tool_schema handler non-string element in names", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["get_tool_schema"]
		if st == nil {
			t.Fatal("get_tool_schema not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{42}},
			},
		})
		if err != nil {
			t.Fatalf("get_tool_schema handler: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result when names contains non-string")
		}
	})

	t.Run("get_tool_schema handler empty names array", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["get_tool_schema"]
		if st == nil {
			t.Fatal("get_tool_schema not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{}},
			},
		})
		if err != nil {
			t.Fatalf("get_tool_schema handler: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result when names is empty")
		}
	})

	t.Run("get_tool_schema handler missing names", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["get_tool_schema"]
		if st == nil {
			t.Fatal("get_tool_schema not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{},
			},
		})
		if err != nil {
			t.Fatalf("get_tool_schema handler: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result when names is missing")
		}
	})

	t.Run("get_tool_schema handler invalid names type", func(t *testing.T) {
		t.Parallel()
		st := proxyServer.mcpServer.ListTools()["get_tool_schema"]
		if st == nil {
			t.Fatal("get_tool_schema not registered")
		}

		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": "not-an-array"},
			},
		})
		if err != nil {
			t.Fatalf("get_tool_schema handler: %v", err)
		}
		if !result.IsError {
			t.Error("expected error result when names is not an array")
		}
	})
}

func TestSyncMCPRegistryDebounce(t *testing.T) {
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

	ctx := context.Background()

	// First sync should succeed and set lastSyncTime.
	if err := proxyServer.syncMCPRegistry(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	firstSync := proxyServer.lastSyncTime
	if firstSync.IsZero() {
		t.Fatal("expected lastSyncTime to be set after first sync")
	}

	// Second sync within debounce window should be skipped.
	if err := proxyServer.syncMCPRegistry(ctx); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !proxyServer.lastSyncTime.Equal(firstSync) {
		t.Error("expected lastSyncTime unchanged within debounce window")
	}

	// Simulate expiry by backdating lastSyncTime.
	proxyServer.lastSyncTime = time.Now().Add(-registrySyncDebounce - time.Second)

	if err := proxyServer.syncMCPRegistry(ctx); err != nil {
		t.Fatalf("third sync after debounce expiry: %v", err)
	}
	if !proxyServer.lastSyncTime.After(firstSync) {
		t.Error("expected lastSyncTime to advance after debounce expiry")
	}

	// InvalidateToolCache should reset debounce so next sync runs immediately.
	thirdSync := proxyServer.lastSyncTime
	proxyServer.InvalidateToolCache()
	if !proxyServer.lastSyncTime.IsZero() {
		t.Error("expected lastSyncTime reset after InvalidateToolCache")
	}

	if err := proxyServer.syncMCPRegistry(ctx); err != nil {
		t.Fatalf("sync after invalidation: %v", err)
	}
	if !proxyServer.lastSyncTime.After(thirdSync) {
		t.Error("expected sync to run after cache invalidation")
	}
}

// newTestProxyWithSession creates a ProxyServer with progressive discovery enabled,
// a real backend, and a registered mock session.
func newTestProxyWithSession(t *testing.T) (*ProxyServer, *mockSession) {
	t.Helper()

	mcpSrv := mcpserver.NewMCPServer("backend-session", "1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("deploy", mcp.WithDescription("Deploy a service to the cluster."), mcp.WithString("service", mcp.Required())),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("deployed"), nil
		},
	)
	mcpSrv.AddTool(
		mcp.NewTool("rollback", mcp.WithDescription("Rollback a deployment."), mcp.WithString("service", mcp.Required())),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("rolled back"), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	backendSrv := httptest.NewTLSServer(mux)
	t.Cleanup(backendSrv.Close)

	record := recordFromServerURL(t, backendSrv.URL)
	record.ID = "backend-session-1"
	record.Name = "k8s"
	record.Status = types.StatusActive

	rootPool := x509.NewCertPool()
	rootPool.AddCert(backendSrv.Certificate())
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootPool}

	capIdx := registration.NewCapabilityIndex()
	record.Capabilities = types.Capability{Tools: []string{"deploy", "rollback"}}
	capIdx.Add(record)

	proxyServer := NewProxyServer(
		capIdx,
		&config.Config{ProgressiveDiscovery: true},
		tlsCfg,
		2*time.Second,
		nil,
	)
	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp: %v", err)
	}
	if err := proxyServer.syncMCPTools(context.Background()); err != nil {
		t.Fatalf("sync tools: %v", err)
	}

	session := newMockSession("test-session-1")
	if err := proxyServer.mcpServer.RegisterSession(context.Background(), session); err != nil {
		t.Fatalf("register session: %v", err)
	}

	return proxyServer, session
}

func TestGetToolSchemaActivatesSessionTools(t *testing.T) {
	t.Parallel()

	t.Run("activates tool in caller session", func(t *testing.T) {
		t.Parallel()
		ps, session := newTestProxyWithSession(t)

		ctx := ps.mcpServer.WithContext(context.Background(), session)
		st := ps.mcpServer.ListTools()["get_tool_schema"]
		if st == nil {
			t.Fatal("get_tool_schema not registered")
		}

		result, err := st.Handler(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"k8s.deploy"}},
			},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected non-error result, got: %v", result.Content)
		}

		// Verify tool was activated in the session.
		sessionTools := session.GetSessionTools()
		if _, ok := sessionTools["k8s.deploy"]; !ok {
			t.Error("expected k8s.deploy to be activated in session tools")
		}
		// rollback was not requested and should not be activated.
		if _, ok := sessionTools["k8s.rollback"]; ok {
			t.Error("k8s.rollback should not be activated (not requested)")
		}
	})

	t.Run("duplicate activation is idempotent", func(t *testing.T) {
		t.Parallel()
		ps, session := newTestProxyWithSession(t)

		ctx := ps.mcpServer.WithContext(context.Background(), session)
		st := ps.mcpServer.ListTools()["get_tool_schema"]

		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"k8s.deploy"}},
			},
		}

		// Call twice.
		if _, err := st.Handler(ctx, req); err != nil {
			t.Fatalf("first call: %v", err)
		}
		if _, err := st.Handler(ctx, req); err != nil {
			t.Fatalf("second call: %v", err)
		}

		sessionTools := session.GetSessionTools()
		count := 0
		for name := range sessionTools {
			if name == "k8s.deploy" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 k8s.deploy entry, got %d", count)
		}
	})

	t.Run("no session context still returns schema", func(t *testing.T) {
		t.Parallel()
		ps, _ := newTestProxyWithSession(t)

		st := ps.mcpServer.ListTools()["get_tool_schema"]

		// Call with bare context (no session).
		result, err := st.Handler(context.Background(), mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"k8s.deploy"}},
			},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatal("expected non-error result")
		}

		// Schema should still be returned.
		var tools []ToolDefinition
		text := result.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &tools); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(tools) != 1 || tools[0].Name != "k8s.deploy" {
			t.Errorf("expected schema for k8s.deploy, got %v", tools)
		}
	})

	t.Run("activation cap enforced", func(t *testing.T) {
		t.Parallel()
		ps, session := newTestProxyWithSession(t)

		// Pre-fill the session with maxActivatedSessionTools tools.
		prefilled := make(map[string]mcpserver.ServerTool, maxActivatedSessionTools)
		for i := range maxActivatedSessionTools {
			name := fmt.Sprintf("fake.tool_%d", i)
			prefilled[name] = mcpserver.ServerTool{
				Tool: mcp.NewTool(name, mcp.WithDescription("filler")),
			}
		}
		session.SetSessionTools(prefilled)

		ctx := ps.mcpServer.WithContext(context.Background(), session)
		st := ps.mcpServer.ListTools()["get_tool_schema"]

		result, err := st.Handler(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"k8s.deploy"}},
			},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatal("expected non-error result even when cap reached")
		}

		// Schema should still be returned.
		var tools []ToolDefinition
		text := result.Content[0].(mcp.TextContent).Text
		if err := json.Unmarshal([]byte(text), &tools); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(tools) != 1 {
			t.Errorf("expected 1 tool in schema response, got %d", len(tools))
		}

		// deploy should NOT have been activated because cap was reached.
		sessionTools := session.GetSessionTools()
		if _, ok := sessionTools["k8s.deploy"]; ok {
			t.Error("k8s.deploy should not be activated when session tool cap is reached")
		}
	})

	t.Run("activation trimmed when batch exceeds remaining cap", func(t *testing.T) {
		t.Parallel()
		ps, session := newTestProxyWithSession(t)

		// Fill session to one slot below the cap.
		prefilled := make(map[string]mcpserver.ServerTool, maxActivatedSessionTools-1)
		for i := range maxActivatedSessionTools - 1 {
			name := fmt.Sprintf("fake.tool_%d", i)
			prefilled[name] = mcpserver.ServerTool{
				Tool: mcp.NewTool(name, mcp.WithDescription("filler")),
			}
		}
		session.SetSessionTools(prefilled)

		ctx := ps.mcpServer.WithContext(context.Background(), session)
		st := ps.mcpServer.ListTools()["get_tool_schema"]

		// Request 2 tools but only 1 slot remains.
		result, err := st.Handler(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"k8s.deploy", "k8s.rollback"}},
			},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatal("expected non-error result")
		}

		sessionTools := session.GetSessionTools()
		activated := 0
		for _, name := range []string{"k8s.deploy", "k8s.rollback"} {
			if _, ok := sessionTools[name]; ok {
				activated++
			}
		}
		if activated != 1 {
			t.Errorf("expected exactly 1 tool activated (trimmed to fit cap), got %d", activated)
		}
		if len(sessionTools) > maxActivatedSessionTools {
			t.Errorf("session tools (%d) exceeded cap (%d)", len(sessionTools), maxActivatedSessionTools)
		}
	})

	t.Run("multiple tools activated at once", func(t *testing.T) {
		t.Parallel()
		ps, session := newTestProxyWithSession(t)

		ctx := ps.mcpServer.WithContext(context.Background(), session)
		st := ps.mcpServer.ListTools()["get_tool_schema"]

		result, err := st.Handler(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      "get_tool_schema",
				Arguments: map[string]any{"names": []any{"k8s.deploy", "k8s.rollback"}},
			},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("expected non-error result")
		}

		sessionTools := session.GetSessionTools()
		for _, name := range []string{"k8s.deploy", "k8s.rollback"} {
			if _, ok := sessionTools[name]; !ok {
				t.Errorf("expected %s to be activated in session tools", name)
			}
		}
	})
}
