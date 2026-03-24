package registration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	servermetrics "github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// CapabilityIndex stores routable server capabilities for tool/resource lookups.
type CapabilityIndex struct {
	mu        sync.RWMutex
	tools     map[string][]types.ServerRecord
	resources map[string][]types.ServerRecord
}

// NewCapabilityIndex creates an empty in-memory capability index.
func NewCapabilityIndex() *CapabilityIndex {
	return &CapabilityIndex{
		tools:     make(map[string][]types.ServerRecord),
		resources: make(map[string][]types.ServerRecord),
	}
}

// ToolCount returns the number of distinct tools in the index.
func (i *CapabilityIndex) ToolCount() int {
	if i == nil {
		return 0
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.tools)
}

// Rebuild replaces index state from a full server snapshot.
func (i *CapabilityIndex) Rebuild(servers []types.ServerRecord) {
	if i == nil {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	i.tools = make(map[string][]types.ServerRecord)
	i.resources = make(map[string][]types.ServerRecord)
	for _, server := range servers {
		if !server.Status.IsRoutable() {
			continue
		}
		i.addLocked(server)
	}
	servermetrics.SetActiveToolCount(len(i.tools))
}

// Add inserts a server into the index.
func (i *CapabilityIndex) Add(server types.ServerRecord) {
	if i == nil || !server.Status.IsRoutable() {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	i.addLocked(server)
	servermetrics.SetActiveToolCount(len(i.tools))
}

func (i *CapabilityIndex) addLocked(server types.ServerRecord) {
	copyServer := cloneServerRecord(server)
	for _, tool := range server.Capabilities.Tools {
		name := strings.TrimSpace(tool)
		if name == "" {
			continue
		}
		i.tools[name] = upsertServer(i.tools[name], copyServer)
	}
	for _, resource := range server.Capabilities.Resources {
		prefix := strings.TrimSpace(resource)
		if prefix == "" {
			continue
		}
		i.resources[prefix] = upsertServer(i.resources[prefix], copyServer)
	}
}

// Remove deletes a server from all index entries.
func (i *CapabilityIndex) Remove(serverID string) {
	if i == nil || strings.TrimSpace(serverID) == "" {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	i.removeLocked(serverID)
}

// RefreshServer fetches a server from the store and updates the index.
// If the server is routable it is added; otherwise it is removed.
func (i *CapabilityIndex) RefreshServer(ctx context.Context, serverID string, serverStore store.ServerStore) error {
	if i == nil || serverStore == nil || strings.TrimSpace(serverID) == "" {
		return fmt.Errorf("refresh server: invalid arguments")
	}

	record, err := serverStore.Get(ctx, serverID)
	if err != nil {
		return fmt.Errorf("refresh server: %w", err)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	i.removeLocked(serverID)
	if record != nil && record.Status.IsRoutable() {
		i.addLocked(*record)
	}
	return nil
}

func (i *CapabilityIndex) removeLocked(serverID string) {
	for tool, servers := range i.tools {
		filtered := removeServer(servers, serverID)
		if len(filtered) == 0 {
			delete(i.tools, tool)
			continue
		}
		i.tools[tool] = filtered
	}
	for prefix, servers := range i.resources {
		filtered := removeServer(servers, serverID)
		if len(filtered) == 0 {
			delete(i.resources, prefix)
			continue
		}
		i.resources[prefix] = filtered
	}
	servermetrics.SetActiveToolCount(len(i.tools))
}

// LookupTool returns servers that expose the requested tool.
func (i *CapabilityIndex) LookupTool(name string) []types.ServerRecord {
	if i == nil {
		return nil
	}

	key := strings.TrimSpace(name)
	if key == "" {
		return nil
	}

	i.mu.RLock()
	defer i.mu.RUnlock()
	return cloneServerRecords(i.tools[key])
}

// LookupResource returns servers for the longest matching URI/resource prefix.
func (i *CapabilityIndex) LookupResource(prefix string) []types.ServerRecord {
	if i == nil {
		return nil
	}

	target := strings.TrimSpace(prefix)
	if target == "" {
		return nil
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	match := ""
	for key := range i.resources {
		if strings.HasPrefix(target, key) && len(key) > len(match) {
			match = key
		}
	}
	if match == "" {
		return nil
	}
	return cloneServerRecords(i.resources[match])
}

// ListTools returns sorted tool names present in the index.
func (i *CapabilityIndex) ListTools() []string {
	if i == nil {
		return nil
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	tools := make([]string, 0, len(i.tools))
	for tool := range i.tools {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools
}

// ListResources returns sorted resource prefixes present in the index.
func (i *CapabilityIndex) ListResources() []string {
	if i == nil {
		return nil
	}

	i.mu.RLock()
	defer i.mu.RUnlock()

	resources := make([]string, 0, len(i.resources))
	for prefix := range i.resources {
		resources = append(resources, prefix)
	}
	sort.Strings(resources)
	return resources
}

func upsertServer(servers []types.ServerRecord, server types.ServerRecord) []types.ServerRecord {
	for idx := range servers {
		if servers[idx].ID == server.ID {
			servers[idx] = cloneServerRecord(server)
			return servers
		}
	}
	return append(servers, cloneServerRecord(server))
}

func removeServer(servers []types.ServerRecord, serverID string) []types.ServerRecord {
	filtered := make([]types.ServerRecord, 0, len(servers))
	for _, server := range servers {
		if server.ID != serverID {
			filtered = append(filtered, server)
		}
	}
	return filtered
}

func cloneServerRecords(servers []types.ServerRecord) []types.ServerRecord {
	if len(servers) == 0 {
		return nil
	}

	cloned := make([]types.ServerRecord, 0, len(servers))
	for _, server := range servers {
		cloned = append(cloned, cloneServerRecord(server))
	}
	return cloned
}

func cloneServerRecord(server types.ServerRecord) types.ServerRecord {
	out := server
	out.Capabilities.Tools = append([]string(nil), server.Capabilities.Tools...)
	out.Capabilities.Resources = append([]string(nil), server.Capabilities.Resources...)
	out.Capabilities.Prompts = append([]string(nil), server.Capabilities.Prompts...)
	if server.LastHeartbeat != nil {
		t := server.LastHeartbeat.UTC()
		out.LastHeartbeat = &t
	}
	out.CreatedAt = server.CreatedAt.UTC()
	out.UpdatedAt = server.UpdatedAt.UTC()
	return out
}
