package proxy

import (
	"context"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	gwserver "github.com/cruvero/mcp-gateway/internal/server"
)

func TestProxyMountedInGatewayToolsListAndCall(t *testing.T) {
	t.Parallel()

	record1, client1, cleanup1 := buildRoutedToolBackend(t, "backend-1", "tool.alpha", "alpha-response")
	defer cleanup1()
	record2, client2, cleanup2 := buildRoutedToolBackend(t, "backend-2", "tool.beta", "beta-response")
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	gateway := gwserver.New(proxyGatewayTestConfig(), nil, nil)
	gateway.MountProxyRoutes(proxyServer.Handler())

	httpServer := httptest.NewServer(gateway.Handler())
	defer httpServer.Close()

	client, err := mcpclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	if err != nil {
		t.Fatalf("new mcp client: %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("start mcp client: %v", err)
	}
	_, err = client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "integration-test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		t.Fatalf("initialize mcp client: %v", err)
	}

	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("tools/list through proxy: %v", err)
	}
	if len(tools.Tools) != 2 {
		t.Fatalf("expected 2 aggregated tools, got %d", len(tools.Tools))
	}
	names := []string{tools.Tools[0].Name, tools.Tools[1].Name}
	if !slices.Contains(names, "backend-1.tool.alpha") || !slices.Contains(names, "backend-2.tool.beta") {
		t.Fatalf("expected namespaced tool names, got %v", names)
	}

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "backend-1.tool.alpha",
			Arguments: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("tools/call through proxy: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one tool result content entry, got %d", len(result.Content))
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content block, got %T", result.Content[0])
	}
	if textContent.Text != "alpha-response" {
		t.Fatalf("expected alpha-response, got %q", textContent.Text)
	}
}

func TestProxyMountedInGatewayProgressiveDiscoveryToolsCallable(t *testing.T) {
	t.Parallel()

	record1, client1, cleanup1 := buildRoutedToolBackend(t, "backend-1", "tool.alpha", "alpha-response")
	defer cleanup1()
	record2, client2, cleanup2 := buildRoutedToolBackend(t, "backend-2", "tool.beta", "beta-response")
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	cfg := &config.Config{ProgressiveDiscovery: true}
	proxyServer := NewProxyServer(index, cfg, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	gateway := gwserver.New(proxyGatewayTestConfig(), nil, nil)
	gateway.MountProxyRoutes(proxyServer.Handler())

	httpServer := httptest.NewServer(gateway.Handler())
	defer httpServer.Close()

	client, err := mcpclient.NewStreamableHttpClient(httpServer.URL + "/mcp")
	if err != nil {
		t.Fatalf("new mcp client: %v", err)
	}
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("start mcp client: %v", err)
	}
	_, err = client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "integration-test-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		t.Fatalf("initialize mcp client: %v", err)
	}

	// tools/list with progressive discovery should return only meta-tools,
	// not backend tools. Backend tools are discoverable via search_tools.
	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("tools/list through proxy: %v", err)
	}

	names := make([]string, len(tools.Tools))
	for i, tool := range tools.Tools {
		names[i] = tool.Name
	}

	// Only search_tools + get_tool_schema (backend tools hidden, request_access filtered)
	if len(tools.Tools) != 2 {
		t.Fatalf("expected 2 meta-tools, got %d: %v", len(tools.Tools), names)
	}
	for _, expected := range []string{"search_tools", "get_tool_schema"} {
		if !slices.Contains(names, expected) {
			t.Fatalf("expected tool %q in list, got %v", expected, names)
		}
	}
	for _, hidden := range []string{"backend-1.tool.alpha", "backend-2.tool.beta"} {
		if slices.Contains(names, hidden) {
			t.Fatalf("backend tool %q should be hidden from tools/list with progressive discovery", hidden)
		}
	}

	// Backend tools must be callable.
	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "backend-1.tool.alpha",
			Arguments: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("tools/call backend tool through proxy: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one tool result content entry, got %d", len(result.Content))
	}
	textContent, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content block, got %T", result.Content[0])
	}
	if textContent.Text != "alpha-response" {
		t.Fatalf("expected alpha-response, got %q", textContent.Text)
	}

	// search_tools must be callable.
	searchResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "search_tools",
			Arguments: map[string]any{"query": "alpha"},
		},
	})
	if err != nil {
		t.Fatalf("tools/call search_tools: %v", err)
	}
	if len(searchResult.Content) == 0 {
		t.Fatal("expected search_tools to return content")
	}

	// get_tool_schema must be callable.
	schemaResult, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_tool_schema",
			Arguments: map[string]any{"names": []any{"backend-1.tool.alpha"}},
		},
	})
	if err != nil {
		t.Fatalf("tools/call get_tool_schema: %v", err)
	}
	if len(schemaResult.Content) == 0 {
		t.Fatal("expected get_tool_schema to return content")
	}
}

func proxyGatewayTestConfig() *config.Config {
	return &config.Config{
		ListenAddr:       ":0",
		DBURL:            "postgres://test-db",
		RateDefault:      10,
		RateBurst:        20,
		CircuitThreshold: 5,
		RetryMax:         3,
	}
}
