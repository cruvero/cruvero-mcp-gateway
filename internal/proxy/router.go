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

	"github.com/cruvero/mcp-gateway/internal/auth"
	"github.com/cruvero/mcp-gateway/internal/config"
	"github.com/cruvero/mcp-gateway/internal/ratelimit"
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
	// ErrServerScopeDenied indicates the caller's server scope does not include the target server.
	ErrServerScopeDenied = errors.New("server scope denied")
)

// ServerRateLimitError indicates a request was rejected by per-server rate limiting.
type ServerRateLimitError struct {
	ServerID   string
	ServerName string
	RetryAfter time.Duration
}

// Error returns the error message.
func (e *ServerRateLimitError) Error() string {
	return fmt.Sprintf("server %s rate limit exceeded (retry after %s)", e.ServerName, e.RetryAfter)
}

// RoutingRequest carries per-request context for routing decisions.
type RoutingRequest struct {
	SessionID string
	ToolName  string
}

// RoutingStrategy selects one backend candidate for a routed request.
type RoutingStrategy interface {
	Select(ctx context.Context, candidates []types.ServerRecord, req *RoutingRequest) (*types.ServerRecord, error)
}

// RoundRobinStrategy selects backends in modulo order.
type RoundRobinStrategy struct {
	counter atomic.Uint64
}

// Select chooses the next backend candidate using round-robin.
func (s *RoundRobinStrategy) Select(_ context.Context, candidates []types.ServerRecord, _ *RoutingRequest) (*types.ServerRecord, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	next := s.counter.Add(1) - 1
	selected := candidates[next%uint64(len(candidates))]
	return &selected, nil
}

// Router performs tool-call routing to backend servers.
type Router struct {
	index   *registration.CapabilityIndex
	clients sync.Map // map[string]*resilience.ResilientClient

	strategy   RoutingStrategy
	logger     *slog.Logger
	limiter    ratelimit.LimiterBackend
	nsResolver *NamespaceResolver

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

// SetLimiter configures the per-server rate limit backend for the router.
func (r *Router) SetLimiter(lb ratelimit.LimiterBackend) {
	if r != nil {
		r.limiter = lb
	}
}

// SetNamespaceResolver configures the namespace resolver for federated name
// resolution in tools/call routing.
func (r *Router) SetNamespaceResolver(ns *NamespaceResolver) {
	if r != nil {
		r.nsResolver = ns
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

	backendToolName, requestedServer := r.resolveFederatedName(name)
	candidates := r.index.LookupTool(backendToolName)
	if requestedServer != "" {
		candidates = filterCandidatesByServer(candidates, requestedServer)
	}
	if len(candidates) == 0 {
		servermetrics.ObserveToolCall(name, backendLabel, "not_found", time.Since(start))
		return nil, fmt.Errorf("route tool: %w: %s", ErrToolNotFound, name)
	}

	selected, err := r.strategy.Select(ctx, candidates, &RoutingRequest{ToolName: name})
	if err != nil {
		servermetrics.ObserveToolCall(name, backendLabel, "routing_error", time.Since(start))
		return nil, fmt.Errorf("route tool: strategy select: %w", err)
	}
	if selected == nil {
		servermetrics.ObserveToolCall(name, backendLabel, "not_found", time.Since(start))
		return nil, fmt.Errorf("route tool: %w: %s", ErrToolNotFound, name)
	}
	backendLabel = strings.TrimSpace(selected.Name)
	if backendLabel == "" {
		backendLabel = strings.TrimSpace(selected.ID)
	}
	span.SetAttributes(attribute.String("backend.name", backendLabel))

	if !auth.CheckServerScope(ctx, selected.Name) {
		servermetrics.ObserveToolCall(name, backendLabel, "scope_denied", time.Since(start))
		return nil, fmt.Errorf("route tool: %w: server %s not in scope", ErrServerScopeDenied, selected.Name)
	}

	if err := r.checkServerRateLimit(ctx, selected); err != nil {
		servermetrics.ObserveToolCall(name, backendLabel, "rate_limited", time.Since(start))
		return nil, err
	}

	client := r.GetOrCreateClient(*selected)
	result, err := client.CallTool(ctx, backendToolName, args)
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

func filterCandidatesByServer(candidates []types.ServerRecord, requested string) []types.ServerRecord {
	filtered := make([]types.ServerRecord, 0, len(candidates))
	for _, c := range candidates {
		if matchesServerHint(c.Name, requested) || matchesServerHint(c.ID, requested) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func matchesServerHint(actual, hint string) bool {
	a := strings.TrimSpace(actual)
	return strings.EqualFold(a, hint) ||
		strings.EqualFold(strings.TrimPrefix(a, "mcp-"), hint)
}

// resolveFederatedName maps a federated tool name back to a raw backend tool
// name and an optional server hint. It supports both the new compact format
// (<displayName>.<rawTool>) and the legacy mcp.<server>.<tool> format.
// When a NamespaceResolver is configured, it is tried first.
func (r *Router) resolveFederatedName(name string) (rawToolName, serverHint string) {
	// Direct index match first (covers deduped names like "k8s.list_pods").
	if candidates := r.index.LookupTool(name); len(candidates) > 0 {
		return name, ""
	}

	// Try namespace resolver when configured and not in reject mode.
	if r.nsResolver != nil && r.nsResolver.Mode() != NamespaceModeReject {
		hint, bare := r.nsResolver.ResolveNamespace(name)
		if hint != "" && bare != "" {
			if candidates := r.index.LookupTool(bare); len(candidates) > 0 {
				return bare, hint
			}
		}
	}

	// New format: <displayName>.<rawTool> — strip first segment as server hint.
	if dotIdx := strings.Index(name, "."); dotIdx > 0 {
		rest := name[dotIdx+1:]
		if candidates := r.index.LookupTool(rest); len(candidates) > 0 {
			return rest, name[:dotIdx]
		}
	}

	// Legacy fallback: mcp.<server>.<tool>.
	if parts := strings.SplitN(name, ".", 3); len(parts) == 3 && parts[0] == "mcp" {
		return parts[2], parts[1]
	}

	return name, ""
}

func (r *Router) checkServerRateLimit(ctx context.Context, server *types.ServerRecord) error {
	// Treat a configured rate limit of 0 or negative as "block all".
	if server.RateLimit != nil && *server.RateLimit <= 0 {
		return &ServerRateLimitError{
			ServerID:   server.ID,
			ServerName: server.Name,
			RetryAfter: 0,
		}
	}

	// A nil limiter backend or nil per-server limit means no per-server rate limiting.
	if r.limiter == nil || server.RateLimit == nil {
		return nil
	}
	burst := *server.RateLimit
	if server.RateBurst != nil && *server.RateBurst > 0 {
		burst = *server.RateBurst
	}
	key := ratelimit.LimiterKey{ClientID: "server:" + server.ID, Route: "global"}
	allowed, _, retryAfter, err := r.limiter.Allow(ctx, key, float64(*server.RateLimit), burst)
	if err != nil {
		return fmt.Errorf("server rate limit check: %w", err)
	}
	if !allowed {
		return &ServerRateLimitError{
			ServerID:   server.ID,
			ServerName: server.Name,
			RetryAfter: retryAfter,
		}
	}
	return nil
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
