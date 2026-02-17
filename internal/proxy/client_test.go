package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestBackendClientCallToolAndListTools(t *testing.T) {
	t.Parallel()

	srv, record, tlsConfig := newBackendTestServer(t, false)
	defer srv.Close()

	client := NewBackendClient(record, tlsConfig, 2*time.Second)
	defer func() {
		_ = client.Close()
	}()

	ctx := context.Background()
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Fatalf("expected tool name echo, got %q", tools[0].Name)
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.IsError {
		t.Fatal("expected successful tool result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(result.Content))
	}
	if strings.TrimSpace(result.Content[0].Text) != "hello" {
		t.Fatalf("expected echoed text hello, got %q", result.Content[0].Text)
	}
}

func TestBackendClientListAndReadResources(t *testing.T) {
	t.Parallel()

	srv, record, tlsConfig := newBackendTestServer(t, false)
	defer srv.Close()

	client := NewBackendClient(record, tlsConfig, 2*time.Second)
	defer func() {
		_ = client.Close()
	}()

	ctx := context.Background()
	resources, err := client.ListResources(ctx)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	content, err := client.ReadResource(ctx, "file://docs/readme")
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if content.URI != "file://docs/readme" {
		t.Fatalf("expected uri file://docs/readme, got %q", content.URI)
	}
	if strings.TrimSpace(content.Text) != "readme-body" {
		t.Fatalf("expected readme-body content, got %q", content.Text)
	}
}

func TestBackendClientTimeout(t *testing.T) {
	t.Parallel()

	srv, record, tlsConfig := newBackendTestServer(t, true)
	defer srv.Close()

	client := NewBackendClient(record, tlsConfig, 50*time.Millisecond)
	defer func() {
		_ = client.Close()
	}()

	ctx := context.Background()
	_, err := client.CallTool(ctx, "echo", map[string]any{"message": "hello"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func newBackendTestServer(t *testing.T, slowTool bool) (*httptest.Server, types.ServerRecord, *tls.Config) {
	t.Helper()

	mcpSrv := mcpserver.NewMCPServer(
		"backend",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithResourceCapabilities(true, true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("echo", mcp.WithDescription("echo input"), mcp.WithString("message", mcp.Required())),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if slowTool {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(300 * time.Millisecond):
				}
			}
			message, err := req.RequireString("message")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(message), nil
		},
	)
	mcpSrv.AddResource(
		mcp.NewResource(
			"file://docs/readme",
			"Readme",
			mcp.WithResourceDescription("Readme resource"),
			mcp.WithMIMEType("text/plain"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      req.Params.URI,
					MIMEType: "text/plain",
					Text:     "readme-body",
				},
			}, nil
		},
	)

	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)

	httpMux := http.NewServeMux()
	httpMux.Handle("/mcp", handler)
	srv := httptest.NewTLSServer(httpMux)

	record := recordFromServerURL(t, srv.URL)
	record.ID = "backend-1"
	record.Status = types.StatusActive

	rootPool := x509.NewCertPool()
	rootPool.AddCert(srv.Certificate())
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootPool,
	}

	return srv, record, tlsConfig
}

func recordFromServerURL(t *testing.T, rawURL string) types.ServerRecord {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	host, portRaw, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split host and port: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	return types.ServerRecord{
		Host: host,
		Port: port,
	}
}
