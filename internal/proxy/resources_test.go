package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestHandleListResourcesAggregatesBackends(t *testing.T) {
	t.Parallel()

	record1, client1, cleanup1 := buildResourceBackend(
		t,
		"server-1",
		[]string{"urn:docs/"},
		map[string]string{"urn:docs/readme": "one"},
	)
	defer cleanup1()
	record2, client2, cleanup2 := buildResourceBackend(
		t,
		"server-2",
		[]string{"urn:wiki/"},
		map[string]string{"urn:wiki/home": "two"},
	)
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	resources, err := proxyServer.handleListResources(context.Background())
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	uris := []string{resources[0].URI, resources[1].URI}
	if !slices.Contains(uris, "urn:docs/readme") || !slices.Contains(uris, "urn:wiki/home") {
		t.Fatalf("expected aggregated uris [urn:docs/readme, urn:wiki/home], got %v", uris)
	}
}

func TestHandleReadResourceLongestPrefixWins(t *testing.T) {
	t.Parallel()

	targetURI := "urn:docs/private/file"
	record1, client1, cleanup1 := buildResourceBackend(
		t,
		"server-1",
		[]string{"urn:docs/"},
		map[string]string{targetURI: "short-prefix"},
	)
	defer cleanup1()
	record2, client2, cleanup2 := buildResourceBackend(
		t,
		"server-2",
		[]string{"urn:docs/private/"},
		map[string]string{targetURI: "long-prefix"},
	)
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	content, err := proxyServer.handleReadResource(context.Background(), targetURI)
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if content.Text != "long-prefix" {
		t.Fatalf("expected longest-prefix backend response long-prefix, got %q", content.Text)
	}
}

func TestHandleReadResourceNoMatch(t *testing.T) {
	t.Parallel()

	proxyServer := NewProxyServer(registration.NewCapabilityIndex(), &config.Config{}, nil, 0, nil)
	_, err := proxyServer.handleReadResource(context.Background(), "urn:missing/item")
	if err == nil {
		t.Fatal("expected resource not found error")
	}
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestHandleListResourcesSkipsFailingBackend(t *testing.T) {
	t.Parallel()

	// Backend 1: healthy
	record1, client1, cleanup1 := buildResourceBackend(
		t,
		"server-1",
		[]string{"urn:docs/"},
		map[string]string{"urn:docs/readme": "one"},
	)
	defer cleanup1()

	// Backend 2: will fail (server closed)
	record2, client2, cleanup2 := buildFailingResourceBackend(t, "server-2", []string{"urn:wiki/"})
	defer cleanup2()

	index := registration.NewCapabilityIndex()
	index.Add(record1)
	index.Add(record2)

	proxyServer := NewProxyServer(index, &config.Config{}, nil, 0, nil)
	proxyServer.clients[record1.ID] = client1
	proxyServer.clients[record2.ID] = client2

	resources, err := proxyServer.handleListResources(context.Background())
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource from healthy backend, got %d", len(resources))
	}
	if resources[0].URI != "urn:docs/readme" {
		t.Fatalf("expected urn:docs/readme, got %q", resources[0].URI)
	}
}

func buildFailingResourceBackend(
	t *testing.T,
	serverID string,
	resourcePrefixes []string,
) (types.ServerRecord, *BackendClient, func()) {
	t.Helper()

	mcpSrv := mcpserver.NewMCPServer(
		"backend-"+serverID,
		"1.0.0",
		mcpserver.WithResourceCapabilities(true, true),
	)
	mcpSrv.AddResource(
		mcp.NewResource("urn:placeholder", "urn:placeholder", mcp.WithResourceDescription("will fail"), mcp.WithMIMEType("text/plain")),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "text/plain", Text: "unreachable"},
			}, nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewTLSServer(mux)

	record := recordFromServerURL(t, srv.URL)
	record.ID = serverID
	record.Name = serverID
	record.Status = types.StatusActive
	record.Capabilities = types.Capability{Resources: resourcePrefixes}

	rootPool := x509.NewCertPool()
	rootPool.AddCert(srv.Certificate())
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootPool,
	}

	client := NewBackendClient(record, tlsConfig, 2*time.Second)

	// Close the server so the client fails on connect.
	srv.Close()

	return record, client, func() { _ = client.Close() }
}

func buildResourceBackend(
	t *testing.T,
	serverID string,
	resourcePrefixes []string,
	resourceContent map[string]string,
) (types.ServerRecord, *BackendClient, func()) {
	t.Helper()

	mcpSrv := mcpserver.NewMCPServer(
		"backend-"+serverID,
		"1.0.0",
		mcpserver.WithResourceCapabilities(true, true),
	)

	for uri, text := range resourceContent {
		uriCopy := uri
		textCopy := text
		mcpSrv.AddResource(
			mcp.NewResource(uriCopy, uriCopy, mcp.WithResourceDescription("resource "+uriCopy), mcp.WithMIMEType("text/plain")),
			func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:      req.Params.URI,
						MIMEType: "text/plain",
						Text:     textCopy,
					},
				}, nil
			},
		)
	}

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewTLSServer(mux)

	record := recordFromServerURL(t, srv.URL)
	record.ID = serverID
	record.Status = types.StatusActive
	record.Capabilities = types.Capability{Resources: resourcePrefixes}

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
