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

	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/registration"
	"github.com/cruvero/mcp-gateway/internal/resilience"
	servermetrics "github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
	selected := candidates[next%uint64(len(candidates))]
	return &selected
}

// Router performs tool-call routing to backend servers.
type Router struct {
	index   *registration.CapabilityIndex
	clients sync.Map // map[string]*resilience.ResilientClient

	strategy RoutingStrategy
	logger   *slog.Logger

	tlsConfig      *tls.Config
	backendTimeout time.Duration
	breakers       *resilience.BreakerRegistry
	retryCfg       resilience.RetryConfig
	circuitTimeout time.Duration
	circuitThresh  int
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

	threshold := 5
	circuitTimeout := 30 * time.Second
	if cfg, err := config.Load(); err == nil {
		threshold = cfg.CircuitThreshold
		circuitTimeout = cfg.CircuitTimeout
	}

	return &Router{
		index:          index,
		strategy:       strategy,
		logger:         logger,
		tlsConfig:      tlsConfig,
		backendTimeout: backendTimeout,
		breakers:       resilience.NewBreakerRegistry(),
		retryCfg:       resilience.DefaultRetryConfig(),
		circuitTimeout: circuitTimeout,
		circuitThresh:  threshold,
	}
}

// Route resolves a tool to a backend and forwards tools/call.
func (r *Router) Route(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error) {
	start := time.Now()
	backendLabel := "none"
	ctx, span := otel.Tracer("mcpgw/proxy").Start(ctx, "proxy.route")
	defer span.End()

	if r == nil || r.index == nil {
		return nil, fmt.Errorf("route tool: router index is not initialized")
	}
	name := strings.TrimSpace(toolName)
	span.SetAttributes(attribute.String("tool.name", name))
	if name == "" {
		return nil, fmt.Errorf("route tool: missing tool name")
	}

	candidates := r.index.LookupTool(name)
	if len(candidates) == 0 {
		servermetrics.ObserveToolCall(name, backendLabel, "not_found", time.Since(start))
		return nil, fmt.Errorf("route tool: %w: %s", ErrToolNotFound, name)
	}

	selected := r.strategy.Select(candidates)
	if selected == nil {
		servermetrics.ObserveToolCall(name, backendLabel, "not_found", time.Since(start))
		return nil, fmt.Errorf("route tool: %w: %s", ErrToolNotFound, name)
	}
	backendLabel = strings.TrimSpace(selected.Name)
	if backendLabel == "" {
		backendLabel = strings.TrimSpace(selected.ID)
	}
	span.SetAttributes(attribute.String("backend.name", backendLabel))

	client := r.GetOrCreateClient(*selected)
	result, err := client.CallTool(ctx, name, args)
	if err != nil {
		errorType := "upstream_call"
		if errors.Is(err, resilience.ErrCircuitOpen) {
			errorType = "circuit_open"
		}
		servermetrics.ObserveUpstreamError(backendLabel, errorType)
		servermetrics.ObserveToolCall(name, backendLabel, "error", time.Since(start))
		span.SetAttributes(attribute.Bool("route.success", false))
		return nil, fmt.Errorf("route tool: backend %s: %w", selected.ID, err)
	}
	servermetrics.ObserveToolCall(name, backendLabel, "success", time.Since(start))
	span.SetAttributes(attribute.Bool("route.success", true))

	return result, nil
}

// GetOrCreateClient returns an existing client or lazily creates one.
func (r *Router) GetOrCreateClient(record types.ServerRecord) *resilience.ResilientClient {
	if existing, ok := r.clients.Load(record.ID); ok {
		if client, castOK := existing.(*resilience.ResilientClient); castOK {
			return client
		}
	}

	backend := NewBackendClient(record, r.tlsConfig, r.backendTimeout)
	backend.logger = r.logger

	breaker := r.breakers.GetOrCreate(record.ID, r.circuitThresh, r.circuitTimeout)
	client := resilience.NewResilientClient(
		backend,
		breaker,
		r.retryCfg,
		r.logger.With(slog.String("backend_id", record.ID)),
	)

	actual, loaded := r.clients.LoadOrStore(record.ID, client)
	cached, _ := actual.(*resilience.ResilientClient)
	if cached != nil {
		if loaded {
			_ = backend.Close()
		}
		return cached
	}

	if loaded {
		_ = backend.Close()
	}

	return client
}

// StoreBackendClient wraps an existing backend client with resilience and stores it for routing.
func (r *Router) StoreBackendClient(record types.ServerRecord, backend *BackendClient) {
	if r == nil || backend == nil {
		return
	}
	breaker := r.breakers.GetOrCreate(record.ID, r.circuitThresh, r.circuitTimeout)
	client := resilience.NewResilientClient(
		backend,
		breaker,
		r.retryCfg,
		r.logger.With(slog.String("backend_id", record.ID)),
	)
	r.clients.Store(record.ID, client)
}
