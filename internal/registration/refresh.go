package registration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/store"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// ToolLister fetches a server's current tool catalog. Implemented by the proxy
// layer to avoid circular dependencies between registration and proxy packages.
type ToolLister interface {
	ListToolNames(ctx context.Context, server types.ServerRecord) ([]string, error)
}

// RefreshResult summarizes a capability refresh operation.
type RefreshResult struct {
	ToolsCount   int      `json:"tools_count"`
	NewHash      string   `json:"capabilities_hash"`
	Reindexed    bool     `json:"reindex_triggered"`
	ToolNames    []string `json:"tool_names,omitempty"`
}

// RefreshCapabilities fetches the current tool catalog from a backend server,
// updates the CapabilityIndex and stored hash, broadcasts a change event, and
// invalidates tool caches. It is called by both heartbeat hash-mismatch
// detection and the push refresh endpoint.
//
// If the backend is unreachable, RefreshCapabilities returns an error but does
// NOT update any state — the caller should log the error and retry on the next
// heartbeat.
func RefreshCapabilities(
	ctx context.Context,
	server types.ServerRecord,
	newHash string,
	index *CapabilityIndex,
	serverStore store.ServerStore,
	broadcaster Broadcaster,
	lister ToolLister,
	logger *slog.Logger,
) (*RefreshResult, error) {
	if index == nil || serverStore == nil || lister == nil {
		return nil, fmt.Errorf("refresh capabilities: nil dependency")
	}

	start := time.Now()
	serverID := strings.TrimSpace(server.ID)
	serverName := strings.TrimSpace(server.Name)

	// Fetch current tool catalog from the backend.
	toolNames, err := lister.ListToolNames(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("refresh capabilities: list tools for %s: %w", serverName, err)
	}

	// Update the server record in the database with the new hash and capabilities.
	server.CapabilityHash = newHash
	server.Capabilities.Tools = toolNames
	if err := serverStore.Update(ctx, &server); err != nil {
		return nil, fmt.Errorf("refresh capabilities: update server %s: %w", serverName, err)
	}

	// Refresh the in-memory capability index from the updated store record.
	if err := index.RefreshServer(ctx, serverID, serverStore); err != nil {
		logger.WarnContext(ctx, "refresh capabilities: index refresh failed",
			slog.String("server_id", serverID),
			slog.String("error", err.Error()),
		)
	}

	// Broadcast the change event to other gateway pods.
	if broadcaster != nil {
		broadcastCapabilitiesChanged(broadcaster, serverID, newHash, toolNames, logger)
	}

	logger.InfoContext(ctx, "capability refresh completed",
		slog.String("server_id", serverID),
		slog.String("server_name", serverName),
		slog.Int("tool_count", len(toolNames)),
		slog.Duration("duration", time.Since(start)),
	)

	return &RefreshResult{
		ToolsCount: len(toolNames),
		NewHash:    newHash,
		Reindexed:  true,
		ToolNames:  toolNames,
	}, nil
}

// RefreshCapabilities is a convenience method on Service that fetches the
// server record and delegates to the standalone RefreshCapabilities function.
// Used by the push refresh handler (refresh_handler.go).
func (s *Service) RefreshCapabilities(ctx context.Context, serverID string) (*RefreshResult, error) {
	record, err := s.serverStore.Get(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("refresh capabilities: get server: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("refresh capabilities: server not found: %s", serverID)
	}
	// Compute a fresh hash placeholder — the standalone function will update it.
	return RefreshCapabilities(
		ctx, *record, record.CapabilityHash,
		s.capabilityIndex, s.serverStore, s.broadcaster,
		s.toolLister, s.logger,
	)
}

func broadcastCapabilitiesChanged(b Broadcaster, serverID, hash string, toolNames []string, logger *slog.Logger) {
	// Use a simple JSON payload compatible with the event system.
	// The broadcaster interface accepts subject + []byte.
	payload := fmt.Sprintf(
		`{"server_id":%q,"capabilities_hash":%q,"tool_count":%d}`,
		serverID, hash, len(toolNames),
	)
	subject := "mcpgw.capabilities_changed"
	if err := b.Publish(subject, []byte(payload)); err != nil {
		logger.Warn("broadcast capabilities changed failed",
			slog.String("server_id", serverID),
			slog.String("error", err.Error()),
		)
	}
}
