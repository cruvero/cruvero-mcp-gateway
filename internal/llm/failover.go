package llm

import (
	"context"
	"fmt"
	"log/slog"
)

// ProviderEntry pairs a provider name with its Client for ordered failover.
type ProviderEntry struct {
	Name   string
	Client Client
}

// FailoverClient tries providers in order, falling back on error.
type FailoverClient struct {
	providers []ProviderEntry
	logger    *slog.Logger
}

// NewFailoverClient creates a failover client that tries providers in order.
func NewFailoverClient(providers []ProviderEntry, logger *slog.Logger) *FailoverClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &FailoverClient{
		providers: providers,
		logger:    logger,
	}
}

// Chat tries each provider in order, returning the first successful response.
func (f *FailoverClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if len(f.providers) == 0 {
		return nil, fmt.Errorf("no LLM providers configured")
	}
	var lastErr error
	for _, p := range f.providers {
		resp, err := p.Client.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		f.logger.Warn("llm provider failed, trying next",
			slog.String("provider", p.Name),
			slog.String("error", err.Error()),
		)
	}
	return nil, fmt.Errorf("all %d LLM providers failed: %w", len(f.providers), lastErr)
}
