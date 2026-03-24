package proxy

import (
	"context"
	"fmt"
	"strings"
)

// CatalogEntry enriches a ToolDefinition with server ownership metadata
// for the platform catalog endpoint.
type CatalogEntry struct {
	ToolDefinition
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
}

// ListCatalogTools returns all backend tools with server metadata, excluding
// gateway meta-tools (search_tools, get_tool_schema, gateway.request_access).
func (p *ProxyServer) ListCatalogTools(ctx context.Context) ([]CatalogEntry, error) {
	if p == nil {
		return nil, fmt.Errorf("list catalog tools: proxy server is nil")
	}

	tools, err := p.handleListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list catalog tools: %w", err)
	}

	entries := make([]CatalogEntry, 0, len(tools))
	for _, tool := range tools {
		if _, isMeta := metaToolNames[tool.Name]; isMeta {
			continue
		}

		def, serverID, serverName, ok := p.toolCache.GetWithMeta(tool.Name)
		if !ok {
			def = tool
		}

		entry := CatalogEntry{
			ToolDefinition: def,
			ServerID:       serverID,
			ServerName:     canonicalCatalogServerName(serverName),
		}
		entry.Name = canonicalCatalogName(entry.Name)
		if entry.Name == "" {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// canonicalCatalogName normalises a federated tool name for the platform
// catalog by trimming whitespace and prepending the "mcp." prefix when absent.
func canonicalCatalogName(federatedName string) string {
	name := strings.TrimSpace(federatedName)
	if name == "" || strings.HasPrefix(name, "mcp.") {
		return name
	}
	return "mcp." + name
}

// canonicalCatalogServerName strips the mcp- convention prefix from a server
// name so the catalog always returns clean display names (k8s, todoist, etc.).
func canonicalCatalogServerName(serverName string) string {
	return strings.TrimPrefix(strings.TrimSpace(serverName), "mcp-")
}
