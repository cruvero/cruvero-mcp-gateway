package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/types"
)

var (
	// ErrToolNotFound indicates no routable backend was found for a tool.
	ErrToolNotFound = errors.New("tool not found")
)

// RoutingStrategy selects one backend candidate for a routed request.
type RoutingStrategy interface {
	Select(candidates []types.ServerRecord) *types.ServerRecord
}

// RoundRobinStrategy selects backends in modulo order.
type RoundRobinStrategy struct {
	counter atomic.Uint64
}

// Select chooses the next backend candidate.
func (s *RoundRobinStrategy) Select(candidates []types.ServerRecord) *types.ServerRecord {
	if len(candidates) == 0 {
		return nil
	}
	next := s.counter.Add(1) - 1
	selected := candidates[int(next%uint64(len(candidates)))]
	return &selected
}

// Router performs tool-call routing to backend servers.
type Router struct {
	index   *registration.CapabilityIndex
	clients sync.Map // map[string]*BackendClient

	strategy RoutingStrategy
	logger   *slog.Logger

	tlsConfig      *tls.Config
	backendTimeout time.Duration
}

// NewRouter creates a proxy router with a selection strategy.
func NewRouter(
	index *registration.CapabilityIndex,
	strategy RoutingStrategy,
	tlsConfig *tls.Config,
	backendTimeout time.Duration,
	logger *slog.Logger,
) *Router {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	if strategy == nil {
		strategy = &RoundRobinStrategy{}
	}
	if backendTimeout <= 0 {
		backendTimeout = defaultBackendTimeout
	}

	return &Router{
		index:          index,
		strategy:       strategy,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
	}
}

// Route resolves a tool to a backend and forwards tools/call.
func (r *Router) Route(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error) {
	if r == nil || r.index == nil {
		return nil, fmt.Errorf("route tool: router index is not initialized")
	}
	name := strings.TrimSpace(toolName)
	if name == "" {
		return nil, fmt.Errorf("route tool: missing tool name")
	}

	candidates := r.index.LookupTool(name)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("route tool: %w: %s", ErrToolNotFound, name)
	}

	selected := r.strategy.Select(candidates)
	if selected == nil {
		return nil, fmt.Errorf("route tool: %w: %s", ErrToolNotFound, name)
	}

	client := r.GetOrCreateClient(*selected)
	result, err := client.CallTool(ctx, name, args)
	if err != nil {
		return nil, fmt.Errorf("route tool: backend %s: %w", selected.ID, err)
	}

	return result, nil
}

// GetOrCreateClient returns an existing client or lazily creates one.
func (r *Router) GetOrCreateClient(record types.ServerRecord) *BackendClient {
	if existing, ok := r.clients.Load(record.ID); ok {
		if client, castOK := existing.(*BackendClient); castOK {
			return client
		}
	}

	client := NewBackendClient(record, r.tlsConfig, r.backendTimeout)
	client.logger = r.logger
	actual, _ := r.clients.LoadOrStore(record.ID, client)
	cached, _ := actual.(*BackendClient)
	if cached != nil {
		return cached
	}
	return client
}
