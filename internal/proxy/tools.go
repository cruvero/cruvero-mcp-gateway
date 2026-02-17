package proxy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
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

	result := make([]ToolDefinition, 0, len(toolNames))
	seen := make(map[string]struct{}, len(toolNames))
	for _, toolName := range toolNames {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}
		if _, exists := seen[toolName]; exists {
			continue
		}

		if cached, ok := p.toolCache.Get(toolName); ok {
			result = append(result, cached)
			seen[toolName] = struct{}{}
			continue
		}

		candidates := p.index.LookupTool(toolName)
		if len(candidates) == 0 {
			continue
		}

		backend := candidates[0]
		client := p.getOrCreateClient(backend)
		defs, err := client.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tools: backend %s: %w", backend.ID, err)
		}

		var selected *ToolDefinition
		for _, def := range defs {
			if def.Name == toolName {
				defCopy := def
				selected = &defCopy
				break
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("list tools: backend %s missing definition for tool %s", backend.ID, toolName)
		}

		p.toolCache.Set(toolName, *selected, backend.ID)
		result = append(result, *selected)
		seen[toolName] = struct{}{}
	}

	return result, nil
}
