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

	serverByID := make(map[string]types.ServerRecord)
	for _, prefix := range prefixes {
		servers := p.index.LookupResource(prefix)
		for _, server := range servers {
			if _, exists := serverByID[server.ID]; !exists {
				serverByID[server.ID] = server
			}
		}
	}

	serverIDs := make([]string, 0, len(serverByID))
	for id := range serverByID {
		serverIDs = append(serverIDs, id)
	}
	sort.Strings(serverIDs)

	resourcesByURI := make(map[string]ResourceDefinition)
	for _, serverID := range serverIDs {
		server := serverByID[serverID]
		client := p.getOrCreateClient(server)

		resources, err := client.ListResources(ctx)
		if err != nil {
			return nil, fmt.Errorf("list resources: backend %s: %w", server.ID, err)
		}
		for _, resource := range resources {
			if _, exists := resourcesByURI[resource.URI]; !exists {
				resourcesByURI[resource.URI] = resource
			}
		}
	}

	uris := make([]string, 0, len(resourcesByURI))
	for uri := range resourcesByURI {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	out := make([]ResourceDefinition, 0, len(uris))
	for _, uri := range uris {
		out = append(out, resourcesByURI[uri])
	}
	return out, nil
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
