package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/types"
)

var (
	// ErrResourceNotFound indicates no routable backend was found for a resource URI.
	ErrResourceNotFound = errors.New("resource not found")
)

// handleListResources aggregates resources from all known backend servers.
func (p *ProxyServer) handleListResources(ctx context.Context) ([]ResourceDefinition, error) {
	if p == nil || p.index == nil {
		return nil, fmt.Errorf("list resources: proxy index is not initialized")
	}

	prefixes := p.index.ListResources()
	if len(prefixes) == 0 {
		return []ResourceDefinition{}, nil
	}

	serverByID := p.deduplicateServers(prefixes)
	serverIDs := sortedKeys(serverByID)

	resourcesByURI, err := p.collectResources(ctx, serverIDs, serverByID)
	if err != nil {
		return nil, err
	}

	uris := sortedMapKeys(resourcesByURI)
	out := make([]ResourceDefinition, 0, len(uris))
	for _, uri := range uris {
		out = append(out, resourcesByURI[uri])
	}
	return out, nil
}

// deduplicateServers builds a unique map of servers from resource prefixes.
func (p *ProxyServer) deduplicateServers(prefixes []string) map[string]types.ServerRecord {
	serverByID := make(map[string]types.ServerRecord)
	for _, prefix := range prefixes {
		for _, srv := range p.index.LookupResource(prefix) {
			if _, exists := serverByID[srv.ID]; !exists {
				serverByID[srv.ID] = srv
			}
		}
	}
	return serverByID
}

// collectResources fetches resources from each server, deduplicating by URI.
func (p *ProxyServer) collectResources(ctx context.Context, serverIDs []string, serverByID map[string]types.ServerRecord) (map[string]ResourceDefinition, error) {
	resourcesByURI := make(map[string]ResourceDefinition)
	for _, serverID := range serverIDs {
		srv := serverByID[serverID]
		client := p.getOrCreateClient(srv)

		resources, err := client.ListResources(ctx)
		if err != nil {
			return nil, fmt.Errorf("list resources: backend %s: %w", srv.ID, err)
		}
		for _, resource := range resources {
			if _, exists := resourcesByURI[resource.URI]; !exists {
				resourcesByURI[resource.URI] = resource
			}
		}
	}
	return resourcesByURI, nil
}

func sortedKeys(m map[string]types.ServerRecord) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(m map[string]ResourceDefinition) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// handleReadResource routes resources/read to the backend matching the longest URI prefix.
func (p *ProxyServer) handleReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	if p == nil || p.index == nil {
		return nil, fmt.Errorf("read resource: proxy index is not initialized")
	}
	trimmedURI := strings.TrimSpace(uri)
	if trimmedURI == "" {
		return nil, fmt.Errorf("read resource: missing uri")
	}

	candidates := p.index.LookupResource(trimmedURI)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("read resource: %w: %s", ErrResourceNotFound, trimmedURI)
	}

	server := candidates[0]
	client := p.getOrCreateClient(server)
	content, err := client.ReadResource(ctx, trimmedURI)
	if err != nil {
		return nil, fmt.Errorf("read resource: backend %s: %w", server.ID, err)
	}
	return content, nil
}
