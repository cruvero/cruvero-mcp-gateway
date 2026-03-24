package registration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/policy"
)

// ClassificationSubscriber reacts to cross-pod classification events
// by invalidating the local classification cache.
type ClassificationSubscriber struct {
	broadcaster Broadcaster
	cache       *policy.ClassificationCache
	logger      *slog.Logger
}

// NewClassificationSubscriber creates a subscriber that keeps the local cache in sync.
func NewClassificationSubscriber(
	broadcaster Broadcaster,
	cache *policy.ClassificationCache,
	logger *slog.Logger,
) *ClassificationSubscriber {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &ClassificationSubscriber{
		broadcaster: broadcaster,
		cache:       cache,
		logger:      logger,
	}
}

// Start subscribes to classification events and processes them until the broadcaster is closed.
func (s *ClassificationSubscriber) Start(ctx context.Context) error {
	if s == nil || s.broadcaster == nil {
		return nil
	}
	return s.broadcaster.Subscribe(SubjectClassificationUpdated, func(data []byte) {
		s.handle(ctx, data)
	})
}

func (s *ClassificationSubscriber) handle(ctx context.Context, data []byte) {
	var evt ClassificationEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		s.logger.ErrorContext(ctx, "unmarshal classification event failed",
			slog.String("error", err.Error()),
		)
		return
	}

	toolName := strings.TrimSpace(evt.ToolName)
	if toolName == "" {
		s.logger.WarnContext(ctx, "classification event missing tool_name")
		return
	}

	if s.cache != nil {
		if toolName == "*" {
			s.cache.InvalidateAll()
			s.logger.DebugContext(ctx, "invalidated entire classification cache",
				slog.String("risk_level", evt.RiskLevel),
			)
		} else {
			s.cache.Invalidate(toolName)
			s.logger.DebugContext(ctx, "invalidated classification cache",
				slog.String("tool", toolName),
				slog.String("risk_level", evt.RiskLevel),
			)
		}
	}
}
