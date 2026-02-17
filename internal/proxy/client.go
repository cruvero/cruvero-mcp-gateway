package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	clienttransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/cruvero/mcp-gateway/internal/resilience"
	"github.com/cruvero/mcp-gateway/internal/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultBackendTimeout = 30 * time.Second
)

// BackendClient manages MCP protocol calls to a single backend server.
type BackendClient struct {
	record     types.ServerRecord
	httpClient *http.Client
	logger     *slog.Logger

	baseURL     string
	clientMu    sync.Mutex
	client      *mcpclient.Client
	initialized bool
}

// NewBackendClient creates a backend client with reusable HTTP transport settings.
func NewBackendClient(record types.ServerRecord, tlsConfig *tls.Config, timeout time.Duration) *BackendClient {
	if timeout <= 0 {
		timeout = defaultBackendTimeout
	}

	baseTransport := resilience.NewTransport(tlsConfig, resilience.DefaultPoolOptions())
	var transport http.RoundTripper = baseTransport
	transport = tracingRoundTripper{base: transport}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	baseURL := fmt.Sprintf("https://%s:%d/mcp", strings.TrimSpace(record.Host), record.Port)
	return &BackendClient{
		record:     record,
		httpClient: httpClient,
		logger:     slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		baseURL:    baseURL,
	}
}

// CallTool invokes tools/call on the backend and normalizes the result.
func (c *BackendClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	ctx, span := otel.Tracer("mcpgw/proxy").Start(ctx, "upstream.call",
		traceWithBackendAttributes(c.record, c.baseURL, name)...,
	)
	defer span.End()

	backendSpanName := fmt.Sprintf("backend.%s.%s", sanitizeSpanSegment(c.record.Name), sanitizeSpanSegment(name))
	ctx, backendSpan := otel.Tracer("mcpgw/proxy").Start(ctx, backendSpanName)
	defer backendSpan.End()

	client, err := c.ensureInitialized(ctx)
	if err != nil {
		span.SetAttributes(attribute.Bool("upstream.success", false))
		return nil, fmt.Errorf("call tool: %w", err)
	}

	result, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	})
	if err != nil {
		span.SetAttributes(attribute.Bool("upstream.success", false))
		return nil, fmt.Errorf("call tool: backend call: %w", err)
	}

	out := &ToolResult{
		Content: make([]ContentBlock, 0, len(result.Content)),
		IsError: result.IsError,
	}
	for _, item := range result.Content {
		switch content := item.(type) {
		case mcp.TextContent:
			out.Content = append(out.Content, ContentBlock{
				Type: content.Type,
				Text: content.Text,
			})
		default:
			bytes, marshalErr := json.Marshal(content)
			if marshalErr != nil {
				span.SetAttributes(attribute.Bool("upstream.success", false))
				return nil, fmt.Errorf("call tool: marshal content block: %w", marshalErr)
			}
			out.Content = append(out.Content, ContentBlock{
				Type: "json",
				Text: string(bytes),
			})
		}
	}
	span.SetAttributes(attribute.Bool("upstream.success", true))

	return out, nil
}

// ListTools invokes tools/list and returns normalized tool definitions.
func (c *BackendClient) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	client, err := c.ensureInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	result, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list tools: backend call: %w", err)
	}

	tools := make([]ToolDefinition, 0, len(result.Tools))
	for _, tool := range result.Tools {
		schema, schemaErr := toolInputSchemaJSON(tool)
		if schemaErr != nil {
			return nil, fmt.Errorf("list tools: encode schema for %s: %w", tool.Name, schemaErr)
		}
		tools = append(tools, ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}
	return tools, nil
}

// ListResources invokes resources/list and returns normalized resource definitions.
func (c *BackendClient) ListResources(ctx context.Context) ([]ResourceDefinition, error) {
	client, err := c.ensureInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}

	result, err := client.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list resources: backend call: %w", err)
	}

	resources := make([]ResourceDefinition, 0, len(result.Resources))
	for _, resource := range result.Resources {
		resources = append(resources, ResourceDefinition{
			URI:         resource.URI,
			Name:        resource.Name,
			Description: resource.Description,
			MimeType:    resource.MIMEType,
		})
	}
	return resources, nil
}

// ReadResource invokes resources/read and returns the first textual content block.
func (c *BackendClient) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	client, err := c.ensureInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("read resource: %w", err)
	}

	result, err := client.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: strings.TrimSpace(uri),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read resource: backend call: %w", err)
	}

	if len(result.Contents) == 0 {
		return nil, fmt.Errorf("read resource: empty content")
	}

	switch content := result.Contents[0].(type) {
	case mcp.TextResourceContents:
		return &ResourceContent{
			URI:      content.URI,
			MimeType: content.MIMEType,
			Text:     content.Text,
		}, nil
	case mcp.BlobResourceContents:
		return &ResourceContent{
			URI:      content.URI,
			MimeType: content.MIMEType,
			Text:     content.Blob,
		}, nil
	default:
		return nil, fmt.Errorf("read resource: unsupported resource content type")
	}
}

// Close closes the underlying MCP client session, if initialized.
func (c *BackendClient) Close() error {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	if c.client == nil {
		return nil
	}
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("close backend client: %w", err)
	}
	c.client = nil
	c.initialized = false
	return nil
}

func (c *BackendClient) ensureInitialized(ctx context.Context) (*mcpclient.Client, error) {
	if c == nil {
		return nil, fmt.Errorf("backend client is nil")
	}
	if strings.TrimSpace(c.record.Host) == "" || c.record.Port <= 0 {
		return nil, fmt.Errorf("invalid backend address")
	}

	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	if c.client == nil {
		client, err := mcpclient.NewStreamableHttpClient(
			c.baseURL,
			clienttransport.WithHTTPBasicClient(c.httpClient),
			clienttransport.WithHTTPTimeout(c.httpClient.Timeout),
		)
		if err != nil {
			return nil, fmt.Errorf("create streamable http client: %w", err)
		}
		c.client = client
	}

	if !c.initialized {
		if err := c.client.Start(ctx); err != nil {
			return nil, fmt.Errorf("start mcp client: %w", err)
		}
		_, err := c.client.Initialize(ctx, mcp.InitializeRequest{
			Params: mcp.InitializeParams{
				ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
				ClientInfo: mcp.Implementation{
					Name:    "cruvero-mcp-gateway",
					Version: "0.1.0",
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("initialize mcp client: %w", err)
		}
		c.initialized = true
	}

	return c.client, nil
}

func toolInputSchemaJSON(tool mcp.Tool) (json.RawMessage, error) {
	if len(tool.RawInputSchema) > 0 {
		out := make([]byte, len(tool.RawInputSchema))
		copy(out, tool.RawInputSchema)
		return out, nil
	}

	bytes, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return nil, err
	}
	if len(bytes) == 0 || string(bytes) == "null" {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(bytes), nil
}

type tracingRoundTripper struct {
	base http.RoundTripper
}

func (t tracingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(req.Context(), propagation.HeaderCarrier(req.Header))
	return base.RoundTrip(req)
}

func sanitizeSpanSegment(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_")
	return replacer.Replace(trimmed)
}

func traceWithBackendAttributes(record types.ServerRecord, endpoint string, tool string) []trace.SpanStartOption {
	return []trace.SpanStartOption{
		trace.WithAttributes(
			attribute.String("backend.id", strings.TrimSpace(record.ID)),
			attribute.String("backend.name", strings.TrimSpace(record.Name)),
			attribute.String("backend.endpoint", strings.TrimSpace(endpoint)),
			attribute.String("tool.name", strings.TrimSpace(tool)),
		),
	}
}
