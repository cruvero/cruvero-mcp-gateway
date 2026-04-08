package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/cruvero/mcp-gateway/internal/identity"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// errToolNotResolved indicates a tool was not found in a backend's definitions,
// allowing the aggregator to try the next candidate backend.
var errToolNotResolved = errors.New("tool not resolved from backend")

const defaultToolCacheTTL = 30 * time.Second

type cachedTool struct {
	definition ToolDefinition
	fetchedAt  time.Time
	serverID   string
	serverName string
}

// ToolCache stores aggregated tool definitions with TTL-based invalidation.
type ToolCache struct {
	mu    sync.RWMutex
	tools map[string]cachedTool
	ttl   time.Duration
}

// NewToolCache creates a tool cache with the provided TTL.
func NewToolCache(ttl time.Duration) *ToolCache {
	if ttl <= 0 {
		ttl = defaultToolCacheTTL
	}
	return &ToolCache{
		tools: make(map[string]cachedTool),
		ttl:   ttl,
	}
}

// Get returns a cached tool definition when present and unexpired.
func (c *ToolCache) Get(name string) (ToolDefinition, bool) {
	if c == nil {
		return ToolDefinition{}, false
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return ToolDefinition{}, false
	}

	c.mu.RLock()
	entry, ok := c.tools[key]
	c.mu.RUnlock()
	if !ok {
		return ToolDefinition{}, false
	}

	if time.Since(entry.fetchedAt) > c.ttl {
		c.mu.Lock()
		delete(c.tools, key)
		c.mu.Unlock()
		return ToolDefinition{}, false
	}

	return entry.definition, true
}

// Set stores a tool definition with server ownership metadata.
func (c *ToolCache) Set(name string, def ToolDefinition, serverID, serverName string) {
	if c == nil {
		return
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return
	}

	c.mu.Lock()
	c.tools[key] = cachedTool{
		definition: def,
		fetchedAt:  time.Now().UTC(),
		serverID:   strings.TrimSpace(serverID),
		serverName: strings.TrimSpace(serverName),
	}
	c.mu.Unlock()
}

// GetWithMeta returns a cached tool definition along with server metadata when
// the entry is present and unexpired.
func (c *ToolCache) GetWithMeta(name string) (ToolDefinition, string, string, bool) {
	if c == nil {
		return ToolDefinition{}, "", "", false
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return ToolDefinition{}, "", "", false
	}

	c.mu.RLock()
	entry, ok := c.tools[key]
	c.mu.RUnlock()
	if !ok {
		return ToolDefinition{}, "", "", false
	}

	if time.Since(entry.fetchedAt) > c.ttl {
		c.mu.Lock()
		delete(c.tools, key)
		c.mu.Unlock()
		return ToolDefinition{}, "", "", false
	}

	return entry.definition, entry.serverID, entry.serverName, true
}

// Invalidate clears all cached entries.
func (c *ToolCache) Invalidate() {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.tools = make(map[string]cachedTool)
	c.mu.Unlock()
}

// InvalidateServer removes all entries sourced from the given server.
func (c *ToolCache) InvalidateServer(serverID string) {
	if c == nil {
		return
	}
	id := strings.TrimSpace(serverID)
	if id == "" {
		return
	}

	c.mu.Lock()
	for name, entry := range c.tools {
		if entry.serverID == id {
			delete(c.tools, name)
		}
	}
	c.mu.Unlock()
}

// handleListTools aggregates tool definitions across active backends.
func (p *ProxyServer) handleListTools(ctx context.Context) ([]ToolDefinition, error) {
	if p == nil || p.index == nil {
		return nil, fmt.Errorf("list tools: proxy index is not initialized")
	}
	if p.toolCache == nil {
		p.toolCache = NewToolCache(defaultToolCacheTTL)
	}

	toolNames := p.index.ListTools()
	if len(toolNames) == 0 {
		return []ToolDefinition{}, nil
	}

	agg := &toolAggregator{
		proxy:       p,
		result:      make([]ToolDefinition, 0, len(toolNames)),
		seen:        make(map[string]struct{}, len(toolNames)),
		backendDefs: make(map[string]map[string]ToolDefinition),
	}

	for _, toolName := range toolNames {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}
		if err := agg.aggregateTool(ctx, toolName); err != nil {
			return nil, err
		}
	}

	return agg.result, nil
}

type toolAggregator struct {
	proxy       *ProxyServer
	result      []ToolDefinition
	seen        map[string]struct{}
	backendDefs map[string]map[string]ToolDefinition
}

func (a *toolAggregator) aggregateTool(ctx context.Context, toolName string) error {
	candidates := a.proxy.index.LookupTool(toolName)
	for _, backend := range candidates {
		federatedName := a.resolveFederatedName(backend, toolName)
		if federatedName == "" {
			continue
		}
		if _, exists := a.seen[federatedName]; exists {
			continue
		}

		if cached, ok := a.proxy.toolCache.Get(federatedName); ok {
			a.result = append(a.result, cached)
			a.seen[federatedName] = struct{}{}
			continue
		}

		def, err := a.resolveFromBackend(ctx, backend, toolName, federatedName)
		if errors.Is(err, errToolNotResolved) {
			continue
		}
		if err != nil {
			return err
		}
		a.result = append(a.result, def)
		a.seen[federatedName] = struct{}{}
	}
	return nil
}

func (a *toolAggregator) resolveFromBackend(ctx context.Context, backend types.ServerRecord, toolName, federatedName string) (ToolDefinition, error) {
	definitionsByName, err := a.proxy.loadBackendDefinitions(ctx, backend, a.backendDefs)
	if err != nil {
		return ToolDefinition{}, err
	}

	selected, ok := definitionsByName[toolName]
	if !ok {
		return ToolDefinition{}, errToolNotResolved
	}

	selected.Name = federatedName
	a.proxy.toolCache.Set(federatedName, selected, backend.ID, backend.Name)
	return selected, nil
}

func (p *ProxyServer) loadBackendDefinitions(
	ctx context.Context,
	backend types.ServerRecord,
	cache map[string]map[string]ToolDefinition,
) (map[string]ToolDefinition, error) {
	if defs, ok := cache[backend.ID]; ok {
		return defs, nil
	}

	client := p.getOrCreateClient(backend)
	defs, err := client.ListTools(ctx)
	if err != nil {
		p.logger.Warn("backend tool listing failed, skipping",
			slog.String("backend_id", backend.ID),
			slog.String("backend_name", backend.Name),
			slog.String("error", err.Error()),
		)
		cache[backend.ID] = map[string]ToolDefinition{}
		return map[string]ToolDefinition{}, nil
	}

	byName := make(map[string]ToolDefinition, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		byName[name] = def
	}
	cache[backend.ID] = byName
	return byName, nil
}

func (a *toolAggregator) resolveFederatedName(server types.ServerRecord, toolName string) string {
	if ns := a.proxy.nsResolver; ns != nil && ns.Mode() != NamespaceModeReject {
		serverName := strings.TrimSpace(server.Name)
		if serverName == "" {
			serverName = strings.TrimSpace(server.ID)
		}
		return ns.ApplyNamespace(toolName, serverName)
	}
	return federatedToolName(server, toolName)
}

func federatedToolName(server types.ServerRecord, toolName string) string {
	base := strings.TrimSpace(toolName)
	if base == "" {
		return ""
	}

	serverName := strings.TrimSpace(server.Name)
	if serverName == "" {
		serverName = strings.TrimSpace(server.ID)
	}
	if serverName == "" {
		return ""
	}

	displayName := strings.TrimPrefix(serverName, "mcp-")

	// If raw tool already starts with the display name, use it directly.
	if strings.HasPrefix(base, displayName+".") {
		return base
	}

	return displayName + "." + base
}

const requestAccessToolName = "gateway.request_access"

// metaToolNames are tools owned by the gateway itself (not backend-proxied).
var metaToolNames = map[string]struct{}{
	"search_tools":        {},
	"get_tool_schema":     {},
	"cruvero.orchestrate": {},
	requestAccessToolName: {},
}

// toolFilterFunc is a server.ToolFilterFunc applied at response time for each
// tools/list request. It restricts the tool set based on the caller's identity
// and role without mutating the shared MCPServer registry.
func (p *ProxyServer) toolFilterFunc(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	filtered := p.applyRoleFilter(ctx, tools)
	if p.config != nil && p.config.ProgressiveDiscovery {
		filtered = onlyMetaTools(filtered)
	}
	return filtered
}

// applyRoleFilter restricts tools based on identity/role (the original filter logic).
func (p *ProxyServer) applyRoleFilter(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	if p.userStore == nil {
		return withoutRequestAccessTool(tools)
	}

	id, ok := identity.FromContext(ctx)
	if !ok || id == nil || id.Type != identity.IdentityOIDC {
		return withoutRequestAccessTool(tools)
	}

	user, err := p.userStore.GetByOIDCSub(ctx, id.ID)
	if err != nil || user == nil {
		return onlyRequestAccessTool(tools)
	}

	switch user.Role {
	case types.RoleAdmin:
		return withoutRequestAccessTool(tools)
	case types.RoleBlocked:
		return onlyRequestAccessTool(tools)
	case types.RoleUser, types.RoleViewer:
		perms, err := p.userStore.GetToolPermissions(ctx, user.ID)
		if err != nil {
			p.logger.Error("tool filter: get permissions failed",
				slog.String("user_id", user.ID),
				slog.String("error", err.Error()),
			)
			return onlyRequestAccessTool(tools)
		}
		if len(perms) == 0 {
			return onlyRequestAccessTool(tools)
		}
		allowed := make(map[string]struct{}, len(perms))
		for _, perm := range perms {
			allowed[perm.ToolName] = struct{}{}
		}
		filtered := make([]mcp.Tool, 0, len(tools))
		for _, tool := range tools {
			if tool.Name == requestAccessToolName {
				continue
			}
			if _, ok := allowed[tool.Name]; ok {
				filtered = append(filtered, tool)
			}
		}
		return filtered
	default:
		return onlyRequestAccessTool(tools)
	}
}

// onlyMetaTools returns only gateway-owned tools (search_tools, get_tool_schema,
// gateway.request_access), hiding backend tools from the tools/list response.
// Backend tools remain registered and callable via tools/call.
func onlyMetaTools(tools []mcp.Tool) []mcp.Tool {
	filtered := make([]mcp.Tool, 0, len(metaToolNames))
	for _, tool := range tools {
		if _, ok := metaToolNames[tool.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// withoutRequestAccessTool returns all tools except the synthetic
// gateway.request_access tool. Used for admin and non-OIDC callers.
func withoutRequestAccessTool(tools []mcp.Tool) []mcp.Tool {
	filtered := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Name != requestAccessToolName {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// onlyRequestAccessTool returns only the gateway.request_access tool from
// the slice. Used for blocked/unknown users.
func onlyRequestAccessTool(tools []mcp.Tool) []mcp.Tool {
	for _, tool := range tools {
		if tool.Name == requestAccessToolName {
			return []mcp.Tool{tool}
		}
	}
	return []mcp.Tool{}
}

// buildRequestAccessTool creates the synthetic gateway.request_access tool
// that signals to the LLM that the user is not authorized.
func buildRequestAccessTool() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name: requestAccessToolName,
			Description: "You are not authorized to use any tools on this gateway. " +
				"Please inform the user that their account does not have tool access permissions " +
				"and they should contact an administrator to request access.",
			RawInputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Handler: func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText(
				"Access denied. Your account does not have permission to use tools. " +
					"Please contact an administrator to request access.",
			), nil
		},
	}
}
