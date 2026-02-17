package server

import (
	"context"
	"encoding/json"

	"github.com/cruvero/mcp-gateway/internal/events"
)

type metricsServerSettingsHandler struct {
	next events.ConfigHandler
}

func (h *metricsServerSettingsHandler) Handle(ctx context.Context, data []byte) error {
	var message events.ServerSettingsConfigMessage
	_ = json.Unmarshal(data, &message)

	if h == nil || h.next == nil {
		return nil
	}

	err := h.next.Handle(ctx, data)
	if err != nil {
		if len(message.Servers) == 0 {
			ObserveServerSettingsRejected("unknown", "apply_failed")
			return err
		}
		for _, server := range message.Servers {
			ObserveServerSettingsRejected(server.ServerName, "apply_failed")
		}
		return err
	}

	for _, server := range message.Servers {
		ObserveServerSettingsApplied(server.ServerName)
		if message.ConfigVersion > 0 {
			SetServerSettingsVersion(server.ServerName, message.ConfigVersion)
		}
	}
	return nil
}
