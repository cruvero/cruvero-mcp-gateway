package proxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// ProxyServer hosts the gateway MCP server and backend client registry.
type ProxyServer struct {
	index     *registration.CapabilityIndex
	clients   map[string]*BackendClient
	toolCache *ToolCache
	config    *config.Config
	logger    *slog.Logger

	tlsConfig      *tls.Config
	backendTimeout time.Duration

	mu                sync.Mutex
	router            *Router
	mcpServer         *server.MCPServer
	streamableHandler *server.StreamableHTTPServer
}

// NewProxyServer creates a new proxy server.
func NewProxyServer(
	index *registration.CapabilityIndex,
	cfg *config.Config,
	tlsConfig *tls.Config,
	backendTimeout time.Duration,
	logger *slog.Logger,
) *ProxyServer {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if backendTimeout <= 0 {
		backendTimeout = defaultBackendTimeout
	}

	return &ProxyServer{
		index:          index,
		clients:        make(map[string]*BackendClient),
		toolCache:      NewToolCache(defaultToolCacheTTL),
		config:         cfg,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
		router:         NewRouter(index, &RoundRobinStrategy{}, tlsConfig, backendTimeout, logger),
	}
}

// SetupMCP initializes the mcp-go server and streamable HTTP transport.
func (p *ProxyServer) SetupMCP() error {
	if p == nil {
		return fmt.Errorf("setup mcp: proxy server is nil")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.streamableHandler != nil {
		return nil
	}

	hooks := &server.Hooks{}
	hooks.AddOnRequestInitialization(func(ctx context.Context, id any, message any) error {
		raw, ok := message.(json.RawMessage)
		if !ok {
			return nil
		}

		var envelope struct {
			Method mcp.MCPMethod `json:"method"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("decode mcp method: %w", err)
		}

		switch envelope.Method {
		case mcp.MethodToolsList, mcp.MethodToolsCall, mcp.MethodResourcesList, mcp.MethodResourcesRead:
			if err := p.syncMCPRegistry(ctx); err != nil {
				return fmt.Errorf("sync mcp registry: %w", err)
			}
		}
		return nil
	})

	mcpServer := server.NewMCPServer(
		"Cruvero MCP Gateway",
		"0.1.0",
		server.WithHooks(hooks),
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
		server.WithResourceRecovery(),
	)

	p.mcpServer = mcpServer
	p.streamableHandler = server.NewStreamableHTTPServer(
		mcpServer,
		server.WithHeartbeatInterval(15*time.Second),
	)

	return nil
}

// Handler returns the MCP HTTP handler for mounting into the main router.
func (p *ProxyServer) Handler() http.Handler {
	if p == nil {
		return http.NotFoundHandler()
	}

	if err := p.SetupMCP(); err != nil {
		p.logger.Error("proxy setup failed", slog.String("error", err.Error()))
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "proxy setup failed", http.StatusInternalServerError)
		})
	}

	return p.streamableHandler
}

func (p *ProxyServer) getOrCreateClient(record types.ServerRecord) *BackendClient {
	p.mu.Lock()
	defer p.mu.Unlock()

	client, ok := p.clients[record.ID]
	if ok {
		if p.router != nil {
			p.router.StoreBackendClient(record, client)
		}
		return client
	}

	client = NewBackendClient(record, p.tlsConfig, p.backendTimeout)
	client.logger = p.logger
	p.clients[record.ID] = client
	if p.router != nil {
		p.router.StoreBackendClient(record, client)
	}
	return client
}

func (p *ProxyServer) syncMCPRegistry(ctx context.Context) error {
	if err := p.syncMCPTools(ctx); err != nil {
		return fmt.Errorf("sync tools: %w", err)
	}
	if err := p.syncMCPResources(ctx); err != nil {
		return fmt.Errorf("sync resources: %w", err)
	}
	return nil
}

func (p *ProxyServer) syncMCPTools(ctx context.Context) error {
	if p.mcpServer == nil {
		return fmt.Errorf("mcp server is not initialized")
	}

	tools, err := p.handleListTools(ctx)
	if err != nil {
		return fmt.Errorf("aggregate tool definitions: %w", err)
	}

	serverTools := make([]server.ServerTool, 0, len(tools))
	for _, tool := range tools {
		tool := tool
		mcpTool := mcp.Tool{
			Name:           tool.Name,
			Description:    tool.Description,
			RawInputSchema: tool.InputSchema,
		}

		serverTools = append(serverTools, server.ServerTool{
			Tool: mcpTool,
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				args := req.GetArguments()
				result, routeErr := p.router.Route(ctx, tool.Name, args)
				if routeErr != nil {
					return nil, fmt.Errorf("route tool %s: %w", tool.Name, routeErr)
				}

				mcpContent := make([]mcp.Content, 0, len(result.Content))
				for _, block := range result.Content {
					contentType := block.Type
					if contentType == "" {
						contentType = "text"
					}
					mcpContent = append(mcpContent, mcp.TextContent{
						Type: contentType,
						Text: block.Text,
					})
				}
				return &mcp.CallToolResult{
					Content: mcpContent,
					IsError: result.IsError,
				}, nil
			},
		})
	}

	p.mcpServer.SetTools(serverTools...)
	return nil
}

func (p *ProxyServer) syncMCPResources(ctx context.Context) error {
	if p.mcpServer == nil {
		return fmt.Errorf("mcp server is not initialized")
	}

	resources, err := p.handleListResources(ctx)
	if err != nil {
		return fmt.Errorf("aggregate resources: %w", err)
	}

	serverResources := make([]server.ServerResource, 0, len(resources))
	for _, resourceDef := range resources {
		resourceDef := resourceDef
		resource := mcp.NewResource(
			resourceDef.URI,
			resourceDef.Name,
			mcp.WithResourceDescription(resourceDef.Description),
			mcp.WithMIMEType(resourceDef.MimeType),
		)

		serverResources = append(serverResources, server.ServerResource{
			Resource: resource,
			Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				content, readErr := p.handleReadResource(ctx, req.Params.URI)
				if readErr != nil {
					return nil, readErr
				}
				return []mcp.ResourceContents{
					mcp.TextResourceContents{
						URI:      content.URI,
						MIMEType: content.MimeType,
						Text:     content.Text,
					},
				}, nil
			},
		})
	}
	p.mcpServer.SetResources(serverResources...)
	p.mcpServer.SetResourceTemplates(server.ServerResourceTemplate{
		Template: mcp.NewResourceTemplate("{+uri}", "proxy-resource-template", mcp.WithTemplateDescription("Proxy routed resources")),
		Handler: func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			content, readErr := p.handleReadResource(ctx, req.Params.URI)
			if readErr != nil {
				return nil, readErr
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      content.URI,
					MIMEType: content.MimeType,
					Text:     content.Text,
				},
			}, nil
		},
	})

	return nil
}
