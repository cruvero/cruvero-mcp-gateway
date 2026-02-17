package resilience

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/cruvero/mcp-gateway/internal/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// BackendCaller defines the backend call needed for resilient tool routing.
type BackendCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*types.ToolResult, error)
}

// ResilientClient wraps backend calls with retry and circuit breaker behaviors.
type ResilientClient struct {
	backend  BackendCaller
	breaker  *CircuitBreaker
	retryCfg RetryConfig
	logger   *slog.Logger
}

// NewResilientClient creates a resilient backend caller.
func NewResilientClient(
	backend BackendCaller,
	breaker *CircuitBreaker,
	retryCfg RetryConfig,
	logger *slog.Logger,
) *ResilientClient {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &ResilientClient{
		backend:  backend,
		breaker:  breaker,
		retryCfg: normalizeRetryConfig(retryCfg),
		logger:   logger,
	}
}

// CallTool runs backend tool calls through retry and circuit breaker.
func (c *ResilientClient) CallTool(ctx context.Context, name string, args map[string]any) (*types.ToolResult, error) {
	if c == nil {
		return nil, fmt.Errorf("resilient call tool: client is nil")
	}
	if c.backend == nil {
		return nil, fmt.Errorf("resilient call tool: backend is nil")
	}
	if c.breaker == nil {
		return nil, fmt.Errorf("resilient call tool: circuit breaker is nil")
	}
	ctx, span := otel.Tracer("mcpgw/resilience").Start(ctx, "resilience.upstream_call")
	defer span.End()
	span.SetAttributes(attribute.String("tool.name", name))

	attempt := 0
	var out *types.ToolResult
	err := Retry(ctx, c.retryCfg, func() error {
		attempt++
		before := c.breaker.State()
		c.logger.Debug("tool call attempt",
			slog.String("tool", name),
			slog.Int("attempt", attempt),
			slog.String("circuit_state", string(before)),
		)

		callErr := c.breaker.Execute(ctx, func() error {
			result, err := c.backend.CallTool(ctx, name, args)
			if err != nil {
				return err
			}
			out = result
			return nil
		})

		after := c.breaker.State()
		if before != after {
			c.logger.Warn("circuit state changed",
				slog.String("tool", name),
				slog.String("from", string(before)),
				slog.String("to", string(after)),
			)
		}
		if callErr != nil {
			c.logger.Warn("tool call attempt failed",
				slog.String("tool", name),
				slog.Int("attempt", attempt),
				slog.String("error", callErr.Error()),
			)
		}
		return callErr
	})
	if err != nil {
		span.SetAttributes(attribute.Bool("resilience.success", false))
		return nil, fmt.Errorf("resilient call tool %s: %w", name, err)
	}
	span.SetAttributes(attribute.Bool("resilience.success", true))
	return out, nil
}
