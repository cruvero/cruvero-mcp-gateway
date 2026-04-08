package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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
	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/orchestrator"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/search"
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
	userStore      store.UserStore

	mu                sync.Mutex
	syncMu            sync.Mutex // guards syncMCPRegistry to prevent concurrent SetTools races
	lastSyncTime      time.Time
	router            *Router
	mcpServer         *server.MCPServer
	streamableHandler *server.StreamableHTTPServer
	discoveryIndex    *DiscoveryIndex
	searchCleanup     func()
	embedderType      string
	orchestrator      *orchestrator.Orchestrator
	nsResolver        *NamespaceResolver
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

	var nsResolver *NamespaceResolver
	if cfg != nil {
		nsResolver = NewNamespaceResolver(cfg.ToolNamespaceMode, cfg.NamespaceSeparator, index)
	}

	router := NewRouter(index, &RoundRobinStrategy{}, tlsConfig, backendTimeout, logger)
	router.SetNamespaceResolver(nsResolver)

	ps := &ProxyServer{
		index:          index,
		clients:        make(map[string]*BackendClient),
		toolCache:      NewToolCache(defaultToolCacheTTL),
		config:         cfg,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
		router:         router,
		nsResolver:     nsResolver,
	}

	if cfg != nil && cfg.ProgressiveDiscovery {
		ps.discoveryIndex = NewDiscoveryIndex()
		engine, cleanup, eType := buildSearchEngine(cfg, logger)
		if engine != nil {
			ps.discoveryIndex.SetEngine(engine)
		}
		ps.searchCleanup = cleanup
		ps.embedderType = eType
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

// SetUserStore wires a user store for per-user tool access enforcement.
func (p *ProxyServer) SetUserStore(userStore store.UserStore) {
	if p == nil {
		return
	}
	p.userStore = userStore
}

// SetOrchestrator wires the orchestration engine for the cruvero.orchestrate meta-tool.
func (p *ProxyServer) SetOrchestrator(o *orchestrator.Orchestrator) {
	if p == nil {
		return
	}
	p.orchestrator = o
}

// Router returns the proxy's underlying router for configuration wiring.
func (p *ProxyServer) Router() *Router {
	if p == nil {
		return nil
	}
	return p.router
}

// ApplyToolMetadata enriches the discovery index with platform-provided metadata.
func (p *ProxyServer) ApplyToolMetadata(metadata []ToolMetadata) {
	if p == nil || p.discoveryIndex == nil {
		return
	}
	p.discoveryIndex.ApplyMetadata(metadata)
}

// InvalidateToolCache clears the entire tool definition cache and resets the
// sync debounce so the next request triggers a fresh registry sync.
func (p *ProxyServer) InvalidateToolCache() {
	if p == nil || p.toolCache == nil {
		return
	}
	p.toolCache.Invalidate()
	p.resetSyncDebounce()
}

// InvalidateToolCacheForServer removes cached tool definitions for a specific
// server and resets the sync debounce so changes take effect immediately.
func (p *ProxyServer) InvalidateToolCacheForServer(serverID string) {
	if p == nil || p.toolCache == nil {
		return
	}
	p.toolCache.InvalidateServer(serverID)
	p.resetSyncDebounce()
}

// resetSyncDebounce clears lastSyncTime so the next syncMCPRegistry call
// performs a full sync instead of being debounced.
func (p *ProxyServer) resetSyncDebounce() {
	p.syncMu.Lock()
	p.lastSyncTime = time.Time{}
	p.syncMu.Unlock()
}

// DiscoveryIndex returns the proxy's discovery index for admin wiring.
func (p *ProxyServer) DiscoveryIndex() *DiscoveryIndex {
	if p == nil {
		return nil
	}
	return p.discoveryIndex
}

// EmbedderType returns the actual embedder type in use (reflects fallback).
func (p *ProxyServer) EmbedderType() string {
	if p == nil {
		return "none"
	}
	return p.embedderType
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
		server.WithToolFilter(p.toolFilterFunc),
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
	if name == "" {
		return body, "", "", false, nil
	}

	displayName := strings.TrimPrefix(server, "mcp-")

	// Already a federated name (new or legacy format) — skip rewrite.
	if strings.HasPrefix(name, displayName+".") || strings.HasPrefix(name, "mcp.") {
		return body, "", "", false, nil
	}

	federated := displayName + "." + name
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

// registrySyncDebounce is the minimum interval between full registry syncs.
const registrySyncDebounce = defaultToolCacheTTL

func (p *ProxyServer) syncMCPRegistry(ctx context.Context) error {
	p.syncMu.Lock()
	defer p.syncMu.Unlock()

	if time.Since(p.lastSyncTime) < registrySyncDebounce {
		return nil
	}
	p.lastSyncTime = time.Now()

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

	toolCap := len(tools) + 1
	if p.config.ProgressiveDiscovery {
		toolCap += 2
	}
	if p.orchestrator != nil {
		toolCap++
	}

	serverTools := make([]server.ServerTool, 0, toolCap)
	for _, tool := range tools {
		serverTools = append(serverTools, p.buildServerTool(tool))
	}

	if p.config.ProgressiveDiscovery {
		serverTools = append(serverTools, p.buildSearchToolsMeta()...)
	}
	if p.orchestrator != nil {
		serverTools = append(serverTools, p.buildOrchestrateToolMeta())
	}

	serverTools = append(serverTools, buildRequestAccessTool())
	p.mcpServer.SetTools(serverTools...)
	return nil
}

func (p *ProxyServer) buildServerTool(tool ToolDefinition) server.ServerTool {
	mcpTool := convertToMCPTool(tool)

	return server.ServerTool{
		Tool:    mcpTool,
		Handler: p.makeToolHandler(tool.Name),
	}
}

func convertToMCPTool(tool ToolDefinition) mcp.Tool {
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
	return mcpTool
}

func (p *ProxyServer) makeToolHandler(toolName string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if p.userStore != nil {
			if denied, msg := p.checkToolPermission(ctx, toolName); denied {
				p.auditToolCall(toolName, "permission denied", true)
				return mcp.NewToolResultError(msg), nil
			}
		}

		args := req.GetArguments()
		result, routeErr := p.router.Route(ctx, toolName, args)
		if routeErr != nil {
			var rateLimitErr *ServerRateLimitError
			if errors.As(routeErr, &rateLimitErr) {
				p.auditToolCall(toolName, "server rate limited", true)
				var msg string
				if rateLimitErr.RetryAfter == 0 {
					msg = fmt.Sprintf("Server %s is blocked by configuration and is not accepting requests. Contact an administrator to re-enable this server.",
						rateLimitErr.ServerName)
				} else {
					msg = fmt.Sprintf("Server %s is rate limited. Retry after %s.",
						rateLimitErr.ServerName, rateLimitErr.RetryAfter)
				}
				return mcp.NewToolResultError(msg), nil
			}
			p.auditToolCall(toolName, "", true)
			return nil, fmt.Errorf("route tool %s: %w", toolName, routeErr)
		}

		preview := ""
		if len(result.Content) > 0 {
			preview = truncate(result.Content[0].Text, 1024)
		}
		p.auditToolCall(toolName, preview, result.IsError)

		return &mcp.CallToolResult{
			Content: convertContentBlocks(result.Content),
			IsError: result.IsError,
		}, nil
	}
}

// checkToolPermission evaluates whether the current caller is allowed to call
// the named tool. Returns (denied=true, message) when the call should be blocked.
// Non-OIDC callers are never denied by this check (API key / mTLS unaffected).
func (p *ProxyServer) checkToolPermission(ctx context.Context, toolName string) (bool, string) {
	id, ok := identity.FromContext(ctx)
	if !ok || id == nil || id.Type != identity.IdentityOIDC {
		return false, ""
	}

	deny := func(reason, msg string) (bool, string) {
		p.auditToolDenied(ctx, toolName, id.ID, id.Metadata["email"], reason)
		return true, msg
	}

	user, err := p.userStore.GetByOIDCSub(ctx, id.ID)
	if err != nil {
		p.logger.Error("user permission check failed",
			slog.String("oidc_sub", id.ID),
			slog.String("tool", toolName),
			slog.String("error", err.Error()),
		)
		return deny("lookup_error", "Permission check failed. Contact an administrator.")
	}
	if user == nil {
		return deny("unregistered", "Your account is not registered. Contact an administrator.")
	}

	switch user.Role {
	case types.RoleAdmin:
		return false, ""
	case types.RoleBlocked:
		return deny("blocked", "Your account has been blocked. Contact an administrator.")
	case types.RoleViewer:
		return deny("read_only", "Your account has read-only access and cannot call tools.")
	case types.RoleUser:
		has, err := p.userStore.HasToolPermission(ctx, user.ID, toolName)
		if err != nil {
			p.logger.Error("tool permission lookup failed",
				slog.String("user_id", user.ID),
				slog.String("tool", toolName),
				slog.String("error", err.Error()),
			)
			return deny("lookup_error", "Permission check failed. Contact an administrator.")
		}
		if !has {
			return deny("no_permission", fmt.Sprintf("You do not have permission to use %s. Contact an administrator to request access.", toolName))
		}
		return false, ""
	default:
		return deny("unknown_role", "Unknown account role. Contact an administrator.")
	}
}

func (p *ProxyServer) auditToolDenied(ctx context.Context, toolName, oidcSub, email, reason string) {
	if p.auditStore == nil {
		return
	}
	_ = p.auditStore.Log(ctx, &types.AuditEntry{
		EventType:  "user.tool_denied",
		ClientID:   oidcSub,
		ServerName: toolName,
		Details: map[string]any{
			"tool":     toolName,
			"oidc_sub": oidcSub,
			"email":    email,
			"reason":   reason,
		},
	})
}

func convertContentBlocks(blocks []ContentBlock) []mcp.Content {
	mcpContent := make([]mcp.Content, 0, len(blocks))
	for _, block := range blocks {
		contentType := block.Type
		if contentType == "" {
			contentType = "text"
		}
		mcpContent = append(mcpContent, mcp.TextContent{
			Type: contentType,
			Text: block.Text,
		})
	}
	return mcpContent
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
		{Tool: searchTool, Handler: p.handleSearchTools},
		{Tool: getToolSchema, Handler: p.handleGetToolSchema},
	}
}

func (p *ProxyServer) buildOrchestrateToolMeta() server.ServerTool {
	orchestrateTool := mcp.Tool{
		Name:        "cruvero.orchestrate",
		Description: "High-level safe intent execution. Gateway handles discovery, activation, RBAC, policy, audit, and Cruvero delegation. Supports plan/execute modes.",
		RawInputSchema: json.RawMessage(`{"type":"object","properties":{"intent":{"type":"string","description":"Natural language goal"},"mode":{"type":"string","enum":["plan","execute"],"default":"execute","description":"plan returns the execution plan; execute runs it"},"safety_level":{"type":"string","enum":["strict","read_only"],"default":"strict","description":"strict allows only read_only/unknown tools; read_only excludes destructive"},"domain":{"type":"string","description":"Optional domain filter for tool discovery"}},"required":["intent"]}`),
	}
	return server.ServerTool{
		Tool:    orchestrateTool,
		Handler: p.handleOrchestrate,
	}
}

func (p *ProxyServer) handleOrchestrate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if p.userStore != nil {
		if denied, msg := p.checkToolPermission(ctx, "cruvero.orchestrate"); denied {
			return mcp.NewToolResultError(msg), nil
		}
	}

	result, isError := p.orchestrator.Handle(ctx, req.GetArguments())
	if isError {
		return mcp.NewToolResultError(result), nil
	}
	return mcp.NewToolResultText(result), nil
}

// ExecuteToolCall runs a tool through the full handler pipeline: RBAC permission
// check, backend routing, and audit logging. This is the same sequence used by
// makeToolHandler for regular MCP tool calls.
func (p *ProxyServer) ExecuteToolCall(ctx context.Context, toolName string, args map[string]any) (string, bool, error) {
	if p.userStore != nil {
		if denied, msg := p.checkToolPermission(ctx, toolName); denied {
			p.auditToolCall(toolName, "permission denied", true)
			return msg, true, nil
		}
	}

	result, routeErr := p.router.Route(ctx, toolName, args)
	if routeErr != nil {
		var rateLimitErr *ServerRateLimitError
		if errors.As(routeErr, &rateLimitErr) {
			p.auditToolCall(toolName, "server rate limited", true)
			var msg string
			if rateLimitErr.RetryAfter == 0 {
				msg = fmt.Sprintf("Server %s is blocked by configuration and is not accepting requests. Contact an administrator to re-enable this server.",
					rateLimitErr.ServerName)
			} else {
				msg = fmt.Sprintf("Server %s is rate limited. Retry after %s.",
					rateLimitErr.ServerName, rateLimitErr.RetryAfter)
			}
			return msg, true, nil
		}
		p.auditToolCall(toolName, "", true)
		return "", false, fmt.Errorf("route tool %s: %w", toolName, routeErr)
	}

	text := ""
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}
	preview := truncate(text, 1024)
	p.auditToolCall(toolName, preview, result.IsError)
	return text, result.IsError, nil
}

// ActivateToolsByName looks up tool definitions by name from the discovery index
// and activates them in the caller's MCP session.
func (p *ProxyServer) ActivateToolsByName(ctx context.Context, names []string) {
	if p == nil || p.discoveryIndex == nil || len(names) == 0 {
		return
	}
	tools := p.discoveryIndex.GetTools(names)
	if len(tools) > 0 {
		p.activateSessionTools(ctx, tools)
	}
}

func (p *ProxyServer) handleSearchTools(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	results, totalMatches := p.discoveryIndex.Search(query, category, 0, limit)

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
}

func (p *ProxyServer) handleGetToolSchema(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	names, err := parseToolNames(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	tools := p.discoveryIndex.GetTools(names)
	bytes, marshalErr := json.Marshal(tools)
	if marshalErr != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal tool definitions: %v", marshalErr)), nil
	}

	// Activate resolved tools in the caller's session so the client can invoke them natively.
	if p.config.ProgressiveDiscovery && len(tools) > 0 {
		p.activateSessionTools(ctx, tools)
	}

	return mcp.NewToolResultText(string(bytes)), nil
}

// maxActivatedSessionTools caps the number of tools that can be dynamically
// activated in a single MCP session to bound resource usage.
const maxActivatedSessionTools = 50

// activateSessionTools registers the given tool definitions into the caller's
// MCP session so the client receives a tools/list_changed notification and can
// invoke them natively. Activation is best-effort: failures are logged but
// never propagated.
func (p *ProxyServer) activateSessionTools(ctx context.Context, tools []ToolDefinition) {
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return
	}

	sessionWithTools, ok := session.(server.SessionWithTools)
	if !ok {
		return
	}

	existing := sessionWithTools.GetSessionTools()
	allowed := maxActivatedSessionTools - len(existing)
	if allowed <= 0 {
		p.logger.Info("session tool activation limit reached",
			slog.String("session_id", session.SessionID()),
			slog.Int("limit", maxActivatedSessionTools),
		)
		return
	}

	newTools := make([]server.ServerTool, 0, len(tools))
	for _, td := range tools {
		if _, already := existing[td.Name]; already {
			continue
		}
		newTools = append(newTools, p.buildServerTool(td))
	}
	if len(newTools) == 0 {
		return
	}
	if len(newTools) > allowed {
		p.logger.Info("trimming session tool activations to enforce limit",
			slog.String("session_id", session.SessionID()),
			slog.Int("requested", len(newTools)),
			slog.Int("allowed", allowed),
			slog.Int("limit", maxActivatedSessionTools),
		)
		newTools = newTools[:allowed]
	}

	if err := p.mcpServer.AddSessionTools(session.SessionID(), newTools...); err != nil {
		p.logger.Warn("failed to activate session tools",
			slog.String("session_id", session.SessionID()),
			slog.String("error", err.Error()),
		)
	}
}

func parseToolNames(req mcp.CallToolRequest) ([]string, error) {
	args := req.GetArguments()
	namesRaw, ok := args["names"]
	if !ok {
		return nil, fmt.Errorf("names parameter is required")
	}
	namesSlice, ok := namesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("names must be an array of strings")
	}
	names := make([]string, 0, len(namesSlice))
	for _, v := range namesSlice {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("names must be an array of strings")
		}
		names = append(names, s)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("names must contain at least one string")
	}
	return names, nil
}

// CloseSearch releases resources held by the search engine (e.g. ONNX model).
func (p *ProxyServer) CloseSearch() {
	if p != nil && p.searchCleanup != nil {
		p.searchCleanup()
	}
}

// buildSearchEngine creates the appropriate search engine based on config.
// Returns (nil, noop, "none") for "substring" engine (uses built-in DiscoveryIndex matching).
// The third return value is the actual embedder type (reflects fallback).
// The caller must call the returned cleanup function when done.
func buildSearchEngine(cfg *config.Config, logger *slog.Logger) (search.Engine, func(), string) {
	noop := func() {}

	switch cfg.SearchEngine {
	case "vector":
		embedder, err := search.NewOnnxEmbedder(cfg.OnnxRuntimePath, cfg.OnnxModelPath, cfg.TokenizerPath)
		if err != nil {
			logger.Warn("onnx embedder init failed, falling back to bm25",
				slog.String("error", err.Error()))
			return search.NewBM25Engine(), noop, "none"
		}
		return search.NewVectorEngine(embedder), func() { _ = embedder.Close() }, "onnx-in-process"
	case "hybrid":
		bm25 := search.NewBM25Engine()
		embedder, err := search.NewOnnxEmbedder(cfg.OnnxRuntimePath, cfg.OnnxModelPath, cfg.TokenizerPath)
		if err != nil {
			logger.Warn("onnx embedder init failed, falling back to bm25",
				slog.String("error", err.Error()))
			return bm25, noop, "none"
		}
		return search.NewHybridEngine(bm25, search.NewVectorEngine(embedder)), func() { _ = embedder.Close() }, "onnx-in-process"
	case "substring":
		return nil, noop, "none"
	default:
		return search.NewBM25Engine(), noop, "none"
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
