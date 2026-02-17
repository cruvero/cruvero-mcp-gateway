package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func TestProxyServerGetOrCreateClientCachesByServerID(t *testing.T) {
	t.Parallel()

	index := registration.NewCapabilityIndex()
	proxyServer := NewProxyServer(index, &config.Config{}, nil, time.Second, nil)

	record := types.ServerRecord{
		ID:     "server-1",
		Name:   "server-1",
		Host:   "127.0.0.1",
		Port:   8443,
		Status: types.StatusActive,
	}
	first := proxyServer.getOrCreateClient(record)
	second := proxyServer.getOrCreateClient(record)
	if first == nil || second == nil {
		t.Fatal("expected backend clients")
	}
	if first != second {
		t.Fatal("expected cached backend client for same server id")
	}
}

func TestProxyServerSyncMCPRegistry(t *testing.T) {
	t.Parallel()

	srv, record, tlsConfig := newBackendTestServer(t, false)
	defer srv.Close()

	record.ID = "backend-1"
	record.Name = "backend-1"
	record.Status = types.StatusActive
	record.Capabilities = types.Capability{
		Tools:     []string{"echo"},
		Resources: []string{"file://docs/readme"},
	}

	index := registration.NewCapabilityIndex()
	index.Add(record)

	proxyServer := NewProxyServer(index, &config.Config{}, tlsConfig, time.Second, nil)
	if err := proxyServer.SetupMCP(); err != nil {
		t.Fatalf("setup mcp: %v", err)
	}
	_ = proxyServer.getOrCreateClient(record)

	if err := proxyServer.syncMCPRegistry(context.Background()); err != nil {
		t.Fatalf("sync mcp registry: %v", err)
	}
}

func TestProxyServerSyncMCPResourcesRequiresSetup(t *testing.T) {
	t.Parallel()

	proxyServer := NewProxyServer(registration.NewCapabilityIndex(), &config.Config{}, nil, time.Second, nil)
	if err := proxyServer.syncMCPResources(context.Background()); err == nil {
		t.Fatal("expected error when mcp server is not initialized")
	}
}

func TestRouterGetOrCreateClientCachesClient(t *testing.T) {
	t.Parallel()

	router := NewRouter(registration.NewCapabilityIndex(), &RoundRobinStrategy{}, nil, time.Second, nil)
	record := types.ServerRecord{
		ID:   "server-1",
		Name: "server-1",
		Host: "127.0.0.1",
		Port: 8443,
	}

	first := router.GetOrCreateClient(record)
	second := router.GetOrCreateClient(record)
	if first == nil || second == nil {
		t.Fatal("expected resilient clients")
	}
	if first != second {
		t.Fatal("expected cached resilient client for same server id")
	}
}

func TestProxyServerSetupMCPNilReceiver(t *testing.T) {
	t.Parallel()

	var proxyServer *ProxyServer
	if err := proxyServer.SetupMCP(); err == nil {
		t.Fatal("expected setup error for nil proxy server")
	}
}

func TestProxyServerSyncRegistryAndResourcesErrorPaths(t *testing.T) {
	t.Parallel()

	proxyServer := NewProxyServer(registration.NewCapabilityIndex(), &config.Config{}, nil, time.Second, nil)
	if err := proxyServer.syncMCPRegistry(context.Background()); err == nil {
		t.Fatal("expected sync registry error when mcp server is not initialized")
	}

	proxyServer.mcpServer = nil
	proxyServer.index = nil
	if err := proxyServer.syncMCPResources(context.Background()); err == nil {
		t.Fatal("expected sync resources error when dependencies are missing")
	}
}

func TestToolInputSchemaJSONBranches(t *testing.T) {
	t.Parallel()

	raw, err := toolInputSchemaJSON(mcp.Tool{RawInputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatalf("schema from raw: %v", err)
	}
	if string(raw) != `{"type":"object"}` {
		t.Fatalf("unexpected raw schema output: %s", string(raw))
	}

	empty, err := toolInputSchemaJSON(mcp.Tool{})
	if err != nil {
		t.Fatalf("schema from empty tool: %v", err)
	}
	if len(empty) == 0 {
		t.Fatal("expected non-empty schema encoding for empty tool input schema")
	}
}

func TestBackendClientEnsureInitializedValidation(t *testing.T) {
	t.Parallel()

	var nilClient *BackendClient
	if _, err := nilClient.ensureInitialized(context.Background()); err == nil {
		t.Fatal("expected nil client initialization error")
	}

	client := NewBackendClient(types.ServerRecord{Host: "", Port: 0}, nil, time.Second)
	if _, err := client.ensureInitialized(context.Background()); err == nil {
		t.Fatal("expected invalid backend address error")
	}
}

func TestBackendClientReadResourceBlobAndEmptyContent(t *testing.T) {
	t.Parallel()

	mcpSrv := mcpserver.NewMCPServer(
		"backend-blob",
		"1.0.0",
		mcpserver.WithResourceCapabilities(true, true),
	)
	mcpSrv.AddResource(
		mcp.NewResource("file://blob", "Blob", mcp.WithMIMEType("application/octet-stream")),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			_ = ctx
			return []mcp.ResourceContents{
				mcp.BlobResourceContents{URI: req.Params.URI, MIMEType: "application/octet-stream", Blob: "ZGF0YQ=="},
			}, nil
		},
	)
	mcpSrv.AddResource(
		mcp.NewResource("file://empty", "Empty", mcp.WithMIMEType("text/plain")),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			_ = ctx
			_ = req
			return []mcp.ResourceContents{}, nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	record := recordFromServerURL(t, srv.URL)
	rootPool := x509.NewCertPool()
	rootPool.AddCert(srv.Certificate())
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootPool}

	client := NewBackendClient(record, tlsConfig, 2*time.Second)
	defer func() { _ = client.Close() }()

	blob, err := client.ReadResource(context.Background(), "file://blob")
	if err != nil {
		t.Fatalf("read blob resource: %v", err)
	}
	if blob.Text != "ZGF0YQ==" {
		t.Fatalf("expected blob payload, got %q", blob.Text)
	}

	if _, err := client.ReadResource(context.Background(), "file://empty"); err == nil {
		t.Fatal("expected empty content error")
	}
}

func TestProxyServerHandlerRoutesResourceReadEndToEnd(t *testing.T) {
	t.Parallel()

	backendSrv, record, tlsConfig := newBackendTestServer(t, false)
	defer backendSrv.Close()

	record.ID = "backend-1"
	record.Name = "backend-1"
	record.Status = types.StatusActive
	record.Capabilities = types.Capability{
		Tools:     []string{"echo"},
		Resources: []string{"file://docs/readme"},
	}

	index := registration.NewCapabilityIndex()
	index.Add(record)

	proxyServer := NewProxyServer(index, &config.Config{}, tlsConfig, time.Second, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", proxyServer.Handler())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := mcpclient.NewStreamableHttpClient(srv.URL + "/mcp")
	if err != nil {
		t.Fatalf("new mcp client: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start mcp client: %v", err)
	}
	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "proxy-test", Version: "1.0.0"},
		},
	}); err != nil {
		t.Fatalf("initialize mcp client: %v", err)
	}

	resources, err := client.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		t.Fatalf("list resources through proxy: %v", err)
	}
	if len(resources.Resources) == 0 {
		t.Fatal("expected proxied resources from backend")
	}

	result, err := client.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: "file://docs/readme"},
	})
	if err != nil {
		t.Fatalf("read proxied resource: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("expected resource content from backend")
	}
}
