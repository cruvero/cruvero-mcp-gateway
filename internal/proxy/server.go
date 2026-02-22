package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/store"
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
	auditStore     store.AuditStore

	mu                sync.Mutex
	router            *Router
	mcpServer         *server.MCPServer
	streamableHandler *server.StreamableHTTPServer
	discoveryIndex    *DiscoveryIndex
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

	ps := &ProxyServer{
		index:          index,
		clients:        make(map[string]*BackendClient),
		toolCache:      NewToolCache(defaultToolCacheTTL),
		config:         cfg,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
		router:         NewRouter(index, &RoundRobinStrategy{}, tlsConfig, backendTimeout, logger),
	}

	if cfg != nil && cfg.ProgressiveDiscovery {
		ps.discoveryIndex = NewDiscoveryIndex()
	}

	return ps
}

// SetAuditStore wires an audit store for tool call result auditing.
func (p *ProxyServer) SetAuditStore(auditStore store.AuditStore) {
	if p == nil {
		return
	}
	p.auditStore = auditStore
}

// ApplyToolMetadata enriches the discovery index with platform-provided metadata.
func (p *ProxyServer) ApplyToolMetadata(metadata []ToolMetadata) {
	if p == nil || p.discoveryIndex == nil {
		return
	}
	p.discoveryIndex.ApplyMetadata(metadata)
}

// DiscoveryIndex returns the proxy's discovery index for admin wiring.
func (p *ProxyServer) DiscoveryIndex() *DiscoveryIndex {
	if p == nil {
		return nil
	}
	return p.discoveryIndex
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			_ = r.Body.Close()

			rewrittenBody := body
			if len(body) > 0 {
				candidate, fromName, toName, changed, rewriteErr := rewriteLegacyToolCallName(body, r.Header.Get("X-MCP-Server"))
				switch {
				case rewriteErr != nil:
					p.logger.Debug("legacy tool call rewrite skipped",
						slog.String("error", rewriteErr.Error()),
					)
				case changed:
					rewrittenBody = candidate
					p.logger.Debug("rewrote legacy tools/call name",
						slog.String("from", fromName),
						slog.String("to", toName),
						slog.String("server_hint", strings.TrimSpace(r.Header.Get("X-MCP-Server"))),
					)
				}
			}

			r.Body = io.NopCloser(bytes.NewReader(rewrittenBody))
			r.ContentLength = int64(len(rewrittenBody))
			r.Header.Set("Content-Length", strconv.Itoa(len(rewrittenBody)))
		}

		p.streamableHandler.ServeHTTP(w, r)
	})
}

func rewriteLegacyToolCallName(body []byte, serverHint string) ([]byte, string, string, bool, error) {
	server := strings.TrimSpace(serverHint)
	if server == "" || strings.EqualFold(server, "federation") {
		return body, "", "", false, nil
	}

	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", "", false, fmt.Errorf("decode request body: %w", err)
	}

	method, _ := envelope["method"].(string)
	if strings.TrimSpace(method) != string(mcp.MethodToolsCall) {
		return body, "", "", false, nil
	}

	params, ok := envelope["params"].(map[string]any)
	if !ok || params == nil {
		return body, "", "", false, nil
	}

	nameRaw, _ := params["name"].(string)
	name := strings.TrimSpace(nameRaw)
	if name == "" || strings.HasPrefix(name, "mcp.") {
		return body, "", "", false, nil
	}

	federated := "mcp." + server + "." + name
	params["name"] = federated

	rewritten, err := json.Marshal(envelope)
	if err != nil {
		return nil, "", "", false, fmt.Errorf("encode rewritten request body: %w", err)
	}
	return rewritten, name, federated, true, nil
}

func (p *ProxyServer) auditToolCall(toolName string, responsePreview string, isError bool) {
	if p.auditStore == nil {
		return
	}
	go func() {
		entry := &types.AuditEntry{
			EventType:  "tool_call_result",
			ServerName: toolName,
			Details: map[string]any{
				"tool":             toolName,
				"response_preview": responsePreview,
				"is_error":         isError,
			},
		}
		if err := p.auditStore.Log(context.Background(), entry); err != nil {
			p.logger.Error("tool call audit log failed", slog.String("error", err.Error()))
		}
	}()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
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

	if p.config.ProgressiveDiscovery && p.discoveryIndex != nil {
		p.discoveryIndex.Index(tools)
	}

	serverTools := make([]server.ServerTool, 0, len(tools))
	for _, tool := range tools {
		mcpTool := mcp.Tool{
			Name:            tool.Name,
			Description:     tool.Description,
			RawInputSchema:  tool.InputSchema,
			RawOutputSchema: tool.OutputSchema,
			DeferLoading:    tool.DeferLoading,
			Meta:            tool.Meta,
		}
		if tool.Annotations != nil {
			mcpTool.Annotations = *tool.Annotations
		}
		if len(tool.Icons) > 0 {
			mcpTool.Icons = make([]mcp.Icon, len(tool.Icons))
			copy(mcpTool.Icons, tool.Icons)
		}
		if tool.Execution != nil {
			execCopy := *tool.Execution
			mcpTool.Execution = &execCopy
		}

		if p.config.ProgressiveDiscovery {
			mcpTool.RawInputSchema = json.RawMessage(`{}`)
			mcpTool.RawOutputSchema = nil
			mcpTool.DeferLoading = true
			mcpTool.Description = extractSummary(tool.Description)
		}

		serverTools = append(serverTools, server.ServerTool{
			Tool: mcpTool,
			Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				args := req.GetArguments()
				result, routeErr := p.router.Route(ctx, tool.Name, args)
				if routeErr != nil {
					p.auditToolCall(tool.Name, "", true)
					return nil, fmt.Errorf("route tool %s: %w", tool.Name, routeErr)
				}

				preview := ""
				if len(result.Content) > 0 {
					preview = truncate(result.Content[0].Text, 1024)
				}
				p.auditToolCall(tool.Name, preview, result.IsError)

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

	if p.config.ProgressiveDiscovery {
		serverTools = append(serverTools, p.buildSearchToolsMeta()...)
	}

	p.mcpServer.SetTools(serverTools...)
	return nil
}

func (p *ProxyServer) buildSearchToolsMeta() []server.ServerTool {
	searchTool := mcp.NewTool("search_tools",
		mcp.WithDescription("Search available tools by keyword. Returns matching tool names with summaries."),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query string")),
		mcp.WithString("category", mcp.Description("Optional server category filter")),
		mcp.WithNumber("limit", mcp.Description("Max results to return (default 10, max 50)")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)

	getToolSchema := mcp.NewTool("get_tool_schema",
		mcp.WithDescription("Retrieve full tool definitions including input schemas for specific tools by name."),
		mcp.WithArray("names",
			mcp.Items(map[string]any{"type": "string"}),
			mcp.Required(),
			mcp.Description("Tool names to retrieve (max 20)"),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)

	return []server.ServerTool{
		{
			Tool: searchTool,
			Handler: func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				args := req.GetArguments()
				query, queryErr := req.RequireString("query")
				if queryErr != nil {
					return mcp.NewToolResultError(fmt.Sprintf("query parameter is required: %v", queryErr)), nil
				}
				category, _ := args["category"].(string)
				limit := defaultSearchLimit
				if v, ok := args["limit"].(float64); ok && v > 0 {
					limit = int(v)
				}

				results, totalMatches := p.discoveryIndex.Search(query, category, limit)

				type searchResult struct {
					Tools        []ToolDefinition `json:"tools"`
					TotalMatches int              `json:"total_matches"`
					Showing      int              `json:"showing"`
				}
				resp := searchResult{
					Tools:        results,
					TotalMatches: totalMatches,
					Showing:      len(results),
				}
				bytes, err := json.Marshal(resp)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("marshal search results: %v", err)), nil
				}
				return mcp.NewToolResultText(string(bytes)), nil
			},
		},
		{
			Tool: getToolSchema,
			Handler: func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				args := req.GetArguments()
				namesRaw, ok := args["names"]
				if !ok {
					return mcp.NewToolResultError("names parameter is required"), nil
				}
				namesSlice, ok := namesRaw.([]any)
				if !ok {
					return mcp.NewToolResultError("names must be an array of strings"), nil
				}
				names := make([]string, 0, len(namesSlice))
				for _, v := range namesSlice {
					s, ok := v.(string)
					if !ok {
						return mcp.NewToolResultError("names must be an array of strings"), nil
					}
					names = append(names, s)
				}
				if len(names) == 0 {
					return mcp.NewToolResultError("names must contain at least one string"), nil
				}

				tools := p.discoveryIndex.GetTools(names)
				bytes, err := json.Marshal(tools)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("marshal tool definitions: %v", err)), nil
				}
				return mcp.NewToolResultText(string(bytes)), nil
			},
		},
	}
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
