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

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/resilience"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestRouterRoutesToCorrectBackend(t *testing.T) {
	t.Parallel()

	record, client, cleanup := buildRoutedToolBackend(t, "server-1", "tool.echo", "server-1")
	defer cleanup()

	index := registration.NewCapabilityIndex()
	index.Add(record)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record.ID, newResilientClientForTest(record.ID, client))

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
	router.clients.Store(record1.ID, newResilientClientForTest(record1.ID, client1))
	router.clients.Store(record2.ID, newResilientClientForTest(record2.ID, client2))

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

func TestRouterRoutesFederatedToolToNamedBackend(t *testing.T) {
	t.Parallel()

	record1, client1, cleanup1 := buildRoutedToolBackend(t, "server-1", "tool.shared", "one")
	defer cleanup1()
	record2, client2, cleanup2 := buildRoutedToolBackend(t, "server-2", "tool.shared", "two")
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record1.ID, newResilientClientForTest(record1.ID, client1))
	router.clients.Store(record2.ID, newResilientClientForTest(record2.ID, client2))

	result, err := router.Route(context.Background(), "mcp.server-2.tool.shared", map[string]any{})
	if err != nil {
		t.Fatalf("route federated tool: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "two" {
		t.Fatalf("expected routed backend result two, got %q", result.Content[0].Text)
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

func newResilientClientForTest(serverID string, client *BackendClient) *resilience.ResilientClient {
	return resilience.NewResilientClient(
		client,
		resilience.NewCircuitBreaker(serverID, 3, time.Second),
		resilience.RetryConfig{
			MaxAttempts:       1,
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        time.Millisecond,
			BackoffMultiplier: 1,
			Jitter:            false,
		},
		nil,
	)
}

func intPtr(v int) *int { return &v }

func TestRouterServerRateLimitEnforced(t *testing.T) {
	t.Parallel()

	record, client, cleanup := buildRoutedToolBackend(t, "server-rl", "tool.echo", "ok")
	defer cleanup()

	record.RateLimit = intPtr(1)
	record.RateBurst = intPtr(1)

	index := registration.NewCapabilityIndex()
	index.Add(record)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record.ID, newResilientClientForTest(record.ID, client))

	backend := ratelimit.NewMemoryBackend(time.Second, 5*time.Second)
	defer func() { _ = backend.Close() }()
	router.SetLimiter(backend)

	// Pre-exhaust the token bucket so the next Route call is rate-limited.
	// This avoids flakiness from the HTTP round-trip to the test backend
	// taking long enough (>1s) for the 1-token/s bucket to refill.
	key := ratelimit.LimiterKey{ClientID: "server:" + record.ID, Route: "global"}
	_, _, _, _ = backend.Allow(context.Background(), key, 1.0, 1)

	// Route should be rejected — token already exhausted.
	_, err := router.Route(context.Background(), "tool.echo", map[string]any{})
	if err == nil {
		t.Fatal("expected rate limit error after token exhaustion")
	}

	var rateLimitErr *ServerRateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected *ServerRateLimitError, got %T: %v", err, err)
	}
	if rateLimitErr.ServerID != record.ID {
		t.Fatalf("expected server ID %q, got %q", record.ID, rateLimitErr.ServerID)
	}
}

func TestRouterServerRateLimitNotConfigured(t *testing.T) {
	t.Parallel()

	record, client, cleanup := buildRoutedToolBackend(t, "server-norl", "tool.echo", "ok")
	defer cleanup()

	// RateLimit and RateBurst left nil — no per-server rate limiting.

	index := registration.NewCapabilityIndex()
	index.Add(record)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record.ID, newResilientClientForTest(record.ID, client))

	backend := ratelimit.NewMemoryBackend(time.Second, 5*time.Second)
	defer func() { _ = backend.Close() }()
	router.SetLimiter(backend)

	// Both calls should succeed — no rate limit configured on server.
	for i := 0; i < 2; i++ {
		_, err := router.Route(context.Background(), "tool.echo", map[string]any{})
		if err != nil {
			t.Fatalf("call %d should succeed without rate limit: %v", i+1, err)
		}
	}
}

func TestRouterServerRateLimitNoLimiter(t *testing.T) {
	t.Parallel()

	record, client, cleanup := buildRoutedToolBackend(t, "server-nolim", "tool.echo", "ok")
	defer cleanup()

	record.RateLimit = intPtr(1)
	record.RateBurst = intPtr(1)

	index := registration.NewCapabilityIndex()
	index.Add(record)

	// No limiter set on the router.
	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record.ID, newResilientClientForTest(record.ID, client))

	// Both calls should succeed — no limiter backend wired.
	for i := 0; i < 2; i++ {
		_, err := router.Route(context.Background(), "tool.echo", map[string]any{})
		if err != nil {
			t.Fatalf("call %d should succeed without limiter: %v", i+1, err)
		}
	}
}

func TestRouterServerRateLimitZeroBlocks(t *testing.T) {
	t.Parallel()

	record, client, cleanup := buildRoutedToolBackend(t, "server-zerolimit", "tool.echo", "blocked")
	defer cleanup()

	// RateLimit = 0 should block all requests for this server.
	record.RateLimit = intPtr(0)

	index := registration.NewCapabilityIndex()
	index.Add(record)

	router := NewRouter(index, &RoundRobinStrategy{}, nil, 0, nil)
	router.clients.Store(record.ID, newResilientClientForTest(record.ID, client))

	backend := ratelimit.NewMemoryBackend(time.Second, 5*time.Second)
	defer func() { _ = backend.Close() }()
	router.SetLimiter(backend)

	for i := range 2 {
		_, err := router.Route(context.Background(), "tool.echo", map[string]any{})
		if err == nil {
			t.Fatalf("call %d should be blocked by zero rate limit", i+1)
		}

		var rlErr *ServerRateLimitError
		if !errors.As(err, &rlErr) {
			t.Fatalf("call %d expected *ServerRateLimitError, got %T: %v", i+1, err, err)
		}
	}
}
