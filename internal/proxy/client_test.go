package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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

func TestBackendClientCallToolHTTPProtocol(t *testing.T) {
	t.Parallel()

	mcpSrv := mcpserver.NewMCPServer(
		"backend-http",
		"1.0.0",
		mcpserver.WithToolCapabilities(true),
	)
	mcpSrv.AddTool(
		mcp.NewTool("echo", mcp.WithDescription("echo input"), mcp.WithString("message", mcp.Required())),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			message, err := req.RequireString("message")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(message), nil
		},
	)
	handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	record := recordFromServerURL(t, srv.URL)
	record.ID = "backend-http-1"
	record.Status = types.StatusActive
	record.Protocol = "http"

	client := NewBackendClient(record, nil, 2*time.Second)
	defer func() { _ = client.Close() }()

	result, err := client.CallTool(context.Background(), "echo", map[string]any{"message": "hello-http"})
	if err != nil {
		t.Fatalf("call tool over http: %v", err)
	}
	if len(result.Content) != 1 || strings.TrimSpace(result.Content[0].Text) != "hello-http" {
		t.Fatalf("unexpected tool result: %+v", result)
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
		Host:     host,
		Port:     port,
		Protocol: strings.ToLower(strings.TrimSpace(parsed.Scheme)),
	}
}

func TestToolDefinitionFieldPreservation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tool       mcp.Tool
		checkDef   func(t *testing.T, def ToolDefinition)
	}{
		{
			name: "all fields populated",
			tool: mcp.NewTool("full-tool",
				mcp.WithDescription("a fully populated tool"),
				mcp.WithString("input", mcp.Required()),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithTitleAnnotation("Full Tool"),
				mcp.WithDeferLoading(true),
				mcp.WithToolIcons(mcp.Icon{Src: "https://example.com/icon.png", MIMEType: "image/png"}),
				mcp.WithTaskSupport(mcp.TaskSupportOptional),
				mcp.WithRawOutputSchema(json.RawMessage(`{"type":"object","properties":{"result":{"type":"string"}}}`)),
			),
			checkDef: func(t *testing.T, def ToolDefinition) {
				t.Helper()
				if def.Name != "full-tool" {
					t.Fatalf("expected name full-tool, got %q", def.Name)
				}
				if def.Description != "a fully populated tool" {
					t.Fatalf("expected description preserved, got %q", def.Description)
				}
				if len(def.InputSchema) == 0 {
					t.Fatal("expected non-empty input schema")
				}
				if len(def.OutputSchema) == 0 {
					t.Fatal("expected non-empty output schema")
				}
				if def.Annotations == nil {
					t.Fatal("expected non-nil annotations")
				}
				if def.Annotations.Title != "Full Tool" {
					t.Fatalf("expected title Full Tool, got %q", def.Annotations.Title)
				}
				if def.Annotations.ReadOnlyHint == nil || !*def.Annotations.ReadOnlyHint {
					t.Fatal("expected read only hint true")
				}
				if !def.DeferLoading {
					t.Fatal("expected defer loading true")
				}
				if len(def.Icons) != 1 || def.Icons[0].Src != "https://example.com/icon.png" {
					t.Fatalf("expected 1 icon, got %+v", def.Icons)
				}
				if def.Execution == nil || def.Execution.TaskSupport != mcp.TaskSupportOptional {
					t.Fatalf("expected execution with optional task support, got %+v", def.Execution)
				}
			},
		},
		{
			name: "minimal fields only",
			tool: mcp.NewTool("minimal",
				mcp.WithDescription("bare minimum"),
				mcp.WithString("input"),
			),
			checkDef: func(t *testing.T, def ToolDefinition) {
				t.Helper()
				if def.Name != "minimal" {
					t.Fatalf("expected name minimal, got %q", def.Name)
				}
				// NewTool sets default annotation hints; annotations should be non-nil
				if def.Annotations == nil {
					t.Fatal("expected non-nil annotations from NewTool defaults")
				}
				if def.DeferLoading {
					t.Fatal("expected defer loading false for minimal tool")
				}
				if def.OutputSchema != nil {
					t.Fatalf("expected nil output schema, got %s", string(def.OutputSchema))
				}
				if len(def.Icons) != 0 {
					t.Fatalf("expected empty icons, got %+v", def.Icons)
				}
				if def.Execution != nil {
					t.Fatalf("expected nil execution, got %+v", def.Execution)
				}
				if def.Meta != nil {
					t.Fatalf("expected nil meta, got %+v", def.Meta)
				}
			},
		},
		{
			name: "zero-value annotations result in nil pointer",
			tool: func() mcp.Tool {
				t := mcp.NewTool("zero-annot", mcp.WithDescription("no annotations set"))
				t.Annotations = mcp.ToolAnnotation{}
				return t
			}(),
			checkDef: func(t *testing.T, def ToolDefinition) {
				t.Helper()
				if def.Annotations != nil {
					t.Fatalf("expected nil annotations for zero-value, got %+v", def.Annotations)
				}
			},
		},
		{
			name: "empty icons slice",
			tool: func() mcp.Tool {
				t := mcp.NewTool("empty-icons", mcp.WithDescription("empty icons slice"))
				t.Icons = []mcp.Icon{}
				return t
			}(),
			checkDef: func(t *testing.T, def ToolDefinition) {
				t.Helper()
				if len(def.Icons) != 0 {
					t.Fatalf("expected empty icons, got %+v", def.Icons)
				}
			},
		},
		{
			name: "nil execution",
			tool: func() mcp.Tool {
				t := mcp.NewTool("nil-exec", mcp.WithDescription("no execution"))
				t.Execution = nil
				return t
			}(),
			checkDef: func(t *testing.T, def ToolDefinition) {
				t.Helper()
				if def.Execution != nil {
					t.Fatalf("expected nil execution, got %+v", def.Execution)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mcpSrv := mcpserver.NewMCPServer("field-test", "1.0.0", mcpserver.WithToolCapabilities(true))
			mcpSrv.AddTool(tt.tool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText("ok"), nil
			})

			handler := mcpserver.NewStreamableHTTPServer(mcpSrv)
			mux := http.NewServeMux()
			mux.Handle("/mcp", handler)
			srv := httptest.NewServer(mux)
			defer srv.Close()

			record := recordFromServerURL(t, srv.URL)
			record.ID = "field-test-" + tt.name
			record.Status = types.StatusActive
			record.Protocol = "http"

			client := NewBackendClient(record, nil, 2*time.Second)
			defer func() { _ = client.Close() }()

			defs, err := client.ListTools(context.Background())
			if err != nil {
				t.Fatalf("list tools: %v", err)
			}
			if len(defs) != 1 {
				t.Fatalf("expected 1 tool, got %d", len(defs))
			}
			tt.checkDef(t, defs[0])
		})
	}
}

func TestToolOutputSchemaJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tool     mcp.Tool
		wantNil  bool
		contains string
	}{
		{
			name: "raw output schema set",
			tool: func() mcp.Tool {
				t := mcp.NewTool("raw-out")
				t.RawOutputSchema = json.RawMessage(`{"type":"object"}`)
				return t
			}(),
			contains: `"type":"object"`,
		},
		{
			name: "typed output schema set",
			tool: func() mcp.Tool {
				t := mcp.NewTool("typed-out")
				t.OutputSchema = mcp.ToolOutputSchema{Type: "string"}
				return t
			}(),
			contains: `"type":"string"`,
		},
		{
			name: "neither set returns nil",
			tool: mcp.NewTool("no-out"),
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := toolOutputSchemaJSON(tt.tool)
			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %s", string(result))
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil output schema")
			}
			if !strings.Contains(string(result), tt.contains) {
				t.Fatalf("expected output schema to contain %q, got %s", tt.contains, string(result))
			}
		})
	}
}

func TestAnnotationsFromTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tool    mcp.Tool
		wantNil bool
		check   func(t *testing.T, a *mcp.ToolAnnotation)
	}{
		{
			name:    "zero-value annotations",
			tool:    mcp.Tool{Name: "zero", Annotations: mcp.ToolAnnotation{}},
			wantNil: true,
		},
		{
			name: "title only",
			tool: mcp.NewTool("titled", mcp.WithTitleAnnotation("My Tool")),
			check: func(t *testing.T, a *mcp.ToolAnnotation) {
				t.Helper()
				if a.Title != "My Tool" {
					t.Fatalf("expected title My Tool, got %q", a.Title)
				}
			},
		},
		{
			name: "read only hint only",
			tool: mcp.NewTool("ro", mcp.WithReadOnlyHintAnnotation(true)),
			check: func(t *testing.T, a *mcp.ToolAnnotation) {
				t.Helper()
				if a.ReadOnlyHint == nil || !*a.ReadOnlyHint {
					t.Fatal("expected read only hint true")
				}
			},
		},
		{
			name: "all annotation fields",
			tool: mcp.NewTool("all",
				mcp.WithTitleAnnotation("All Hints"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(true),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
			),
			check: func(t *testing.T, a *mcp.ToolAnnotation) {
				t.Helper()
				if a.Title != "All Hints" {
					t.Fatalf("expected title All Hints, got %q", a.Title)
				}
				if a.ReadOnlyHint == nil || *a.ReadOnlyHint {
					t.Fatal("expected read only hint false")
				}
				if a.DestructiveHint == nil || !*a.DestructiveHint {
					t.Fatal("expected destructive hint true")
				}
				if a.IdempotentHint == nil || !*a.IdempotentHint {
					t.Fatal("expected idempotent hint true")
				}
				if a.OpenWorldHint == nil || *a.OpenWorldHint {
					t.Fatal("expected open world hint false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := annotationsFromTool(tt.tool)
			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil annotations")
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
