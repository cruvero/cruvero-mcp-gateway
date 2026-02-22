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

const (
	errHeartbeatFmt = "heartbeat: %w"

	// HeartbeatActionNone indicates no client-side corrective action is required.
	HeartbeatActionNone = "none"
	// HeartbeatActionReRegister indicates the backend must perform a full
	// register handshake because the lease is missing or mismatched.
	HeartbeatActionReRegister = "re_register"
	// HeartbeatActionResendCapabilities indicates the gateway-side capability
	// hash no longer matches the backend declaration.
	HeartbeatActionResendCapabilities = "resend_capabilities"
)

// HeartbeatRequest is the optional payload sent by backends during heartbeat.
type HeartbeatRequest struct {
	Status         string            `json:"status"`
	Metadata       map[string]string `json:"metadata"`
	RegistrationID string            `json:"registration_id,omitempty"`
	LeaseEpoch     int64             `json:"lease_epoch,omitempty"`
	CapabilityHash string            `json:"capability_hash,omitempty"`
}

// HeartbeatResponse is returned after processing a heartbeat.
type HeartbeatResponse struct {
	ServerStatus      types.ServerStatus          `json:"server_status"`
	NextDeadline      time.Time                   `json:"next_deadline"`
	LeaseEpoch        int64                       `json:"lease_epoch"`
	SyncState         types.RegistrationSyncState `json:"sync_state"`
	Action            string                      `json:"action"`
	ConfigVersion     int64                       `json:"config_version"`
	EffectiveSettings map[string]any              `json:"effective_settings"`
}

// Heartbeat records liveness and updates lifecycle status.
func (s *Service) Heartbeat(ctx context.Context, caller *identitypkg.Identity, id string, req HeartbeatRequest) (*HeartbeatResponse, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("heartbeat: %w: missing id", ErrInvalidRequest)
	}
	if caller == nil || caller.Type != identitypkg.IdentityMTLS {
		return nil, fmt.Errorf(errHeartbeatFmt, ErrUnauthorized)
	}

	record, err := s.serverStore.Get(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf(errHeartbeatFmt, ErrNotFound)
		}
		return nil, fmt.Errorf("heartbeat: get registration: %w", err)
	}

	if caller.ID != record.SPIFFEID {
		return nil, fmt.Errorf(errHeartbeatFmt, ErrForbidden)
	}

	action := HeartbeatActionNone
	requestedRegistrationID := strings.TrimSpace(req.RegistrationID)
	if requestedRegistrationID != "" && requestedRegistrationID != record.ID {
		action = HeartbeatActionReRegister
	}
	if req.LeaseEpoch > 0 && req.LeaseEpoch != record.LeaseEpoch {
		action = HeartbeatActionReRegister
	}

	intervalSeconds := heartbeatIntervalSeconds(s.config)
	nextDeadline := time.Now().UTC().Add(time.Duration(intervalSeconds) * time.Second)
	configVersion, effectiveSettings := s.effectiveSettingsForServer(record.Name)
	if action == HeartbeatActionReRegister {
		return &HeartbeatResponse{
			ServerStatus:      record.Status,
			NextDeadline:      nextDeadline,
			LeaseEpoch:        record.LeaseEpoch,
			SyncState:         record.SyncState,
			Action:            action,
			ConfigVersion:     configVersion,
			EffectiveSettings: effectiveSettings,
		}, nil
	}

	if reqHash := strings.TrimSpace(req.CapabilityHash); reqHash != "" && reqHash != strings.TrimSpace(record.CapabilityHash) {
		action = HeartbeatActionResendCapabilities
	}

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
		s.publishBroadcast("status_changed", record.ID)
	}

	if err := s.serverStore.UpdateHeartbeat(ctx, id); err != nil {
		return nil, fmt.Errorf("heartbeat: update heartbeat: %w", err)
	}

	syncState := record.SyncState
	if syncState == "" {
		syncState = types.SyncStateUnacked
	}
	return &HeartbeatResponse{
		ServerStatus:      nextStatus,
		NextDeadline:      nextDeadline,
		LeaseEpoch:        record.LeaseEpoch,
		SyncState:         syncState,
		Action:            action,
		ConfigVersion:     configVersion,
		EffectiveSettings: effectiveSettings,
	}, nil
}
