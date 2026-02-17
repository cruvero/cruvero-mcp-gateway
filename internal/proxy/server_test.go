package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
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
