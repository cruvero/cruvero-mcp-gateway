package registration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	identitypkg "github.com/cruvero/mcp-gateway/internal/identity"
	servermetrics "github.com/cruvero/mcp-gateway/internal/server"
	"github.com/cruvero/mcp-gateway/internal/types"
)

// HeartbeatRequest is the optional payload sent by backends during heartbeat.
type HeartbeatRequest struct {
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata"`
}

// HeartbeatResponse is returned after processing a heartbeat.
type HeartbeatResponse struct {
	ServerStatus      types.ServerStatus `json:"server_status"`
	NextDeadline      time.Time          `json:"next_deadline"`
	ConfigVersion     int64              `json:"config_version"`
	EffectiveSettings map[string]any     `json:"effective_settings"`
}

// Heartbeat records liveness and updates lifecycle status.
func (s *Service) Heartbeat(ctx context.Context, caller *identitypkg.Identity, id string, req HeartbeatRequest) (*HeartbeatResponse, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("heartbeat: %w: missing id", ErrInvalidRequest)
	}
	if caller == nil || caller.Type != identitypkg.IdentityMTLS {
		return nil, fmt.Errorf("heartbeat: %w", ErrUnauthorized)
	}

	record, err := s.serverStore.Get(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("heartbeat: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("heartbeat: get registration: %w", err)
	}

	if caller.ID != record.SPIFFEID {
		return nil, fmt.Errorf("heartbeat: %w", ErrForbidden)
	}

	_ = req
	nextStatus, err := Transition(record.Status, EventHeartbeat)
	if err != nil {
		return nil, fmt.Errorf("heartbeat: %w: %v", ErrInvalidRequest, err)
	}

	if nextStatus != record.Status {
		if err := s.serverStore.UpdateStatus(ctx, id, nextStatus); err != nil {
			return nil, fmt.Errorf("heartbeat: update status: %w", err)
		}
		servermetrics.AddActiveRegistrations(record.Status.String(), -1)
		servermetrics.AddActiveRegistrations(nextStatus.String(), 1)
		if s.publisher != nil {
			if err := s.publisher.PublishServerHealthChanged(ctx, record.ID, record.Name, record.Status, nextStatus); err != nil {
				s.logger.ErrorContext(ctx, "publish server health changed event failed", "error", err.Error())
			}
		}
	}

	if err := s.serverStore.UpdateHeartbeat(ctx, id); err != nil {
		return nil, fmt.Errorf("heartbeat: update heartbeat: %w", err)
	}

	intervalSeconds := heartbeatIntervalSeconds(s.config)
	nextDeadline := time.Now().UTC().Add(time.Duration(intervalSeconds) * time.Second)
	configVersion, effectiveSettings := s.effectiveSettingsForServer(record.Name)
	return &HeartbeatResponse{
		ServerStatus:      nextStatus,
		NextDeadline:      nextDeadline,
		ConfigVersion:     configVersion,
		EffectiveSettings: effectiveSettings,
	}, nil
}
