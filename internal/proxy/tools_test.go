package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestToolCacheHitReturnsCachedDefinition(t *testing.T) {
	t.Parallel()

	cache := NewToolCache(1 * time.Minute)
	cache.Set("tool.echo", ToolDefinition{Name: "tool.echo"}, "server-1")

	def, ok := cache.Get("tool.echo")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if def.Name != "tool.echo" {
		t.Fatalf("expected cached tool name tool.echo, got %q", def.Name)
	}
}

func TestToolCacheInvalidateClearsEntries(t *testing.T) {
	t.Parallel()

	cache := NewToolCache(1 * time.Minute)
	cache.Set("tool.alpha", ToolDefinition{Name: "tool.alpha"}, "server-1")
	cache.Set("tool.beta", ToolDefinition{Name: "tool.beta"}, "server-2")

	cache.Invalidate()
	if _, ok := cache.Get("tool.alpha"); ok {
		t.Fatal("expected tool.alpha to be invalidated")
	}
	if _, ok := cache.Get("tool.beta"); ok {
		t.Fatal("expected tool.beta to be invalidated")
	}
}

func TestToolCacheInvalidateServer(t *testing.T) {
	t.Parallel()

	cache := NewToolCache(1 * time.Minute)
	cache.Set("tool.alpha", ToolDefinition{Name: "tool.alpha"}, "server-1")
	cache.Set("tool.beta", ToolDefinition{Name: "tool.beta"}, "server-2")

	cache.InvalidateServer("server-1")
	if _, ok := cache.Get("tool.alpha"); ok {
		t.Fatal("expected tool.alpha to be invalidated for server-1")
	}
	if _, ok := cache.Get("tool.beta"); !ok {
		t.Fatal("expected tool.beta to remain cached")
	}
}

func TestHandleListToolsCacheMissThenHit(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int64
	record, client, cleanup := buildToolBackendClient(t, "server-1", "tool.echo", "echo tool", &listCalls)
	defer cleanup()

	index := registration.NewCapabilityIndex()
	index.Add(record)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record.ID] = client

	first, err := proxyServer.handleListTools(context.Background())
	if err != nil {
		t.Fatalf("first list tools: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 tool on first call, got %d", len(first))
	}
	if listCalls.Load() != 1 {
		t.Fatalf("expected one backend list call on cache miss, got %d", listCalls.Load())
	}

	second, err := proxyServer.handleListTools(context.Background())
	if err != nil {
		t.Fatalf("second list tools: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 tool on second call, got %d", len(second))
	}
	if listCalls.Load() != 1 {
		t.Fatalf("expected no additional backend list call on cache hit, got %d", listCalls.Load())
	}
}

func TestHandleListToolsCacheExpiryTriggersRefetch(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int64
	record, client, cleanup := buildToolBackendClient(t, "server-1", "tool.echo", "echo tool", &listCalls)
	defer cleanup()

	index := registration.NewCapabilityIndex()
	index.Add(record)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record.ID] = client
	proxyServer.toolCache = NewToolCache(20 * time.Millisecond)

	if _, err := proxyServer.handleListTools(context.Background()); err != nil {
		t.Fatalf("first list tools: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := proxyServer.handleListTools(context.Background()); err != nil {
		t.Fatalf("second list tools after cache expiry: %v", err)
	}

	if listCalls.Load() != 2 {
		t.Fatalf("expected two backend list calls after cache expiry, got %d", listCalls.Load())
	}
}

func TestHandleListToolsExposesConflictsByBackendNamespace(t *testing.T) {
	t.Parallel()

	var firstCalls atomic.Int64
	var secondCalls atomic.Int64

	record1, client1, cleanup1 := buildToolBackendClient(t, "server-1", "tool.same", "first", &firstCalls)
	defer cleanup1()
	record2, client2, cleanup2 := buildToolBackendClient(t, "server-2", "tool.same", "second", &secondCalls)
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	tools, err := proxyServer.handleListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected both backend-scoped tools, got %d", len(tools))
	}
	names := []string{tools[0].Name, tools[1].Name}
	if !slices.Contains(names, "mcp.server-1.tool.same") || !slices.Contains(names, "mcp.server-2.tool.same") {
		t.Fatalf("expected namespaced conflict tools, got %v", names)
	}
	if firstCalls.Load() != 1 {
		t.Fatalf("expected first backend queried once, got %d", firstCalls.Load())
	}
	if secondCalls.Load() != 1 {
		t.Fatalf("expected second backend queried once, got %d", secondCalls.Load())
	}
}

func TestHandleListToolsAggregatesMultipleBackends(t *testing.T) {
	t.Parallel()

	var firstCalls atomic.Int64
	var secondCalls atomic.Int64

	record1, client1, cleanup1 := buildToolBackendClient(t, "server-1", "tool.alpha", "alpha", &firstCalls)
	defer cleanup1()
	record2, client2, cleanup2 := buildToolBackendClient(t, "server-2", "tool.beta", "beta", &secondCalls)
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	tools, err := proxyServer.handleListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := []string{tools[0].Name, tools[1].Name}
	if !slices.Contains(names, "mcp.server-1.tool.alpha") || !slices.Contains(names, "mcp.server-2.tool.beta") {
		t.Fatalf("expected aggregated namespaced tools [mcp.server-1.tool.alpha, mcp.server-2.tool.beta], got %v", names)
	}
}

func buildToolBackendClient(
	t *testing.T,
	serverID string,
	toolName string,
	description string,
	listCounter *atomic.Int64,
) (types.ServerRecord, *BackendClient, func()) {
	t.Helper()

	hooks := &mcpserver.Hooks{}
	if listCounter != nil {
		hooks.AddBeforeListTools(func(ctx context.Context, id any, message *mcp.ListToolsRequest) {
			listCounter.Add(1)
		})
	}

	mcpSrv := mcpserver.NewMCPServer(
		"backend-"+serverID,
		"1.0.0",
		mcpserver.WithHooks(hooks),
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool(toolName, mcp.WithDescription(description), mcp.WithString("message")),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			message := req.GetString("message", "ok")
			return mcp.NewToolResultText(message), nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewTLSServer(mux)

	record := recordFromServerURL(t, srv.URL)
	record.ID = serverID
	record.Status = types.StatusActive
	record.Capabilities = types.Capability{Tools: []string{toolName}}

	rootPool := x509.NewCertPool()
	rootPool.AddCert(srv.Certificate())
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootPool,
	}

	client := NewBackendClient(record, tlsConfig, 2*time.Second)
	return record, client, func() {
		_ = client.Close()
		srv.Close()
	}
}
