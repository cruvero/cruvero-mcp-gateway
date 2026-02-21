package registration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/store"
)

// RegistrationSubscriber reacts to cross-pod registration events
// by refreshing the local capability index.
type RegistrationSubscriber struct {
	broadcaster Broadcaster
	index       *CapabilityIndex
	serverStore store.ServerStore
	logger      *slog.Logger
}

// NewRegistrationSubscriber creates a subscriber that keeps the local index in sync.
func NewRegistrationSubscriber(
	broadcaster Broadcaster,
	index *CapabilityIndex,
	serverStore store.ServerStore,
	logger *slog.Logger,
) *RegistrationSubscriber {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &RegistrationSubscriber{
		broadcaster: broadcaster,
		index:       index,
		serverStore: serverStore,
		logger:      logger,
	}
}

// Start subscribes to registration events and processes them until the broadcaster is closed.
func (s *RegistrationSubscriber) Start(ctx context.Context) error {
	if s == nil || s.broadcaster == nil {
		return nil
	}
	return s.broadcaster.Subscribe(SubjectRegistryUpdated, func(data []byte) {
		s.handle(ctx, data)
	})
}

func (s *RegistrationSubscriber) handle(ctx context.Context, data []byte) {
	var evt RegistrationEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		s.logger.ErrorContext(ctx, "unmarshal registration event failed",
			slog.String("error", err.Error()),
		)
		return
	}

	serverID := strings.TrimSpace(evt.ServerID)
	if serverID == "" {
		s.logger.WarnContext(ctx, "registration event missing server_id")
		return
	}

	switch evt.EventType {
	case "registered", "status_changed":
		if err := s.index.RefreshServer(ctx, serverID, s.serverStore); err != nil {
			s.logger.ErrorContext(ctx, "refresh server in index failed",
				slog.String("event_type", evt.EventType),
				slog.String("server_id", serverID),
				slog.String("error", err.Error()),
			)
		}
	case "deregistered":
		s.index.Remove(serverID)
	default:
		s.logger.WarnContext(ctx, "unknown registration event type",
			slog.String("event_type", evt.EventType),
		)
	}
}
