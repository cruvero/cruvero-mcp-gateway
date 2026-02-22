package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const defaultToolCacheTTL = 30 * time.Second

type cachedTool struct {
	definition ToolDefinition
	fetchedAt  time.Time
	serverID   string
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
func (c *ToolCache) Set(name string, def ToolDefinition, serverID string) {
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
	}
	c.mu.Unlock()
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
		federatedName := federatedToolName(backend, toolName)
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
		return ToolDefinition{}, fmt.Errorf("list tools: backend %s missing definition for tool %s", backend.ID, toolName)
	}

	selected.Name = federatedName
	a.proxy.toolCache.Set(federatedName, selected, backend.ID)
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
		return nil, fmt.Errorf("list tools: backend %s: %w", backend.ID, err)
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

func federatedToolName(server types.ServerRecord, toolName string) string {
	base := strings.TrimSpace(toolName)
	if base == "" {
		return ""
	}
	if strings.HasPrefix(base, "mcp.") {
		return base
	}

	serverName := strings.TrimSpace(server.Name)
	if serverName == "" {
		serverName = strings.TrimSpace(server.ID)
	}
	if serverName == "" {
		return ""
	}

	return "mcp." + serverName + "." + base
}
