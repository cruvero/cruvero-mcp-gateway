package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestRouterRoutesToCorrectBackend(t *testing.T) {
	t.Parallel()

	record, client, cleanup := buildRoutedToolBackend(t, "server-1", "tool.echo", "server-1")
	defer cleanup()

	index := registration.NewCapabilityIndex()
	index.Add(record)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record.ID, client)

	result, err := router.Route(context.Background(), "tool.echo", map[string]any{})
	if err != nil {
		t.Fatalf("route tool: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "server-1" {
		t.Fatalf("expected routed backend result server-1, got %q", result.Content[0].Text)
	}
}

func TestRouterRoundRobinDistribution(t *testing.T) {
	t.Parallel()

	record1, client1, cleanup1 := buildRoutedToolBackend(t, "server-1", "tool.shared", "one")
	defer cleanup1()
	record2, client2, cleanup2 := buildRoutedToolBackend(t, "server-2", "tool.shared", "two")
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record1.ID, client1)
	router.clients.Store(record2.ID, client2)

	counts := map[string]int{"one": 0, "two": 0}
	for i := 0; i < 6; i++ {
		result, err := router.Route(context.Background(), "tool.shared", map[string]any{})
		if err != nil {
			t.Fatalf("route tool iteration %d: %v", i, err)
		}
		if len(result.Content) != 1 {
			t.Fatalf("expected one content block, got %d", len(result.Content))
		}
		counts[result.Content[0].Text]++
	}

	if counts["one"] != 3 || counts["two"] != 3 {
		t.Fatalf("expected round robin 3/3 distribution, got %#v", counts)
	}
}

func TestRouterToolNotFound(t *testing.T) {
	t.Parallel()

	router := NewRouter(registration.NewCapabilityIndex(), &RoundRobinStrategy{}, nil, 0, nil)
	_, err := router.Route(context.Background(), "tool.missing", map[string]any{})
	if err == nil {
		t.Fatal("expected tool not found error")
	}
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("expected ErrToolNotFound, got %v", err)
	}
}

func buildRoutedToolBackend(
	t *testing.T,
	serverID string,
	toolName string,
	responseText string,
) (types.ServerRecord, *BackendClient, func()) {
	t.Helper()

	mcpSrv := mcpserver.NewMCPServer(
		"backend-"+serverID,
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool(toolName, mcp.WithDescription("routed tool")),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(responseText), nil
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
