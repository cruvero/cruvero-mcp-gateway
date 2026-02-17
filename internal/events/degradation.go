package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// DegradationStatus describes events-layer connectivity health.
type DegradationStatus string

const (
	// DegradationStatusConnected means NATS is connected.
	DegradationStatusConnected DegradationStatus = "connected"
	// DegradationStatusDegraded means NATS is disconnected but gateway is serving with last-known config.
	DegradationStatusDegraded DegradationStatus = "degraded"
	// DegradationStatusDisconnected means NATS has not connected yet.
	DegradationStatusDisconnected DegradationStatus = "disconnected"
)

// DegradationManager tracks NATS connectivity and cached config fallback state.
type DegradationManager struct {
	client      *Client
	configStore ConfigStore
	subscriber  *Subscriber
	logger      *slog.Logger

	mu                    sync.RWMutex
	status                DegradationStatus
	everConnected         bool
	hasCachedConfig       bool
	lastKnownVersions     map[string]int64
	settingsSyncStatus    string
	settingsConfigVersion int64
}

// NewDegradationManager creates a degradation manager.
func NewDegradationManager(client *Client, configStore ConfigStore, subscriber *Subscriber, logger *slog.Logger) *DegradationManager {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	status := DegradationStatusDisconnected
	everConnected := false
	if client != nil && client.IsConnected() {
		status = DegradationStatusConnected
		everConnected = true
	}

	return &DegradationManager{
		client:                client,
		configStore:           configStore,
		subscriber:            subscriber,
		logger:                logger,
		status:                status,
		everConnected:         everConnected,
		lastKnownVersions:     make(map[string]int64),
		settingsSyncStatus:    "unknown",
		settingsConfigVersion: 0,
	}
}

// OnDisconnect marks the system degraded while preserving existing in-memory config.
func (m *DegradationManager) OnDisconnect() {
	if m == nil {
		return
	}

	m.mu.Lock()
	m.status = DegradationStatusDegraded
	m.mu.Unlock()

	m.logger.Warn("nats disconnected; running in degraded mode with last-known config")
}

// OnReconnect publishes a full snapshot request and marks the system connected.
func (m *DegradationManager) OnReconnect(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	gatewayID := ""
	if m.client != nil {
		gatewayID = m.client.GatewayID()
	}

	m.mu.Lock()
	versions := copyVersionMap(m.lastKnownVersions)
	m.status = DegradationStatusConnected
	m.everConnected = true
	m.settingsSyncStatus = "resync_requested"
	m.mu.Unlock()

	request := snapshotRequestMessage{
		GatewayID:         gatewayID,
		LastKnownVersions: versions,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("on reconnect: marshal snapshot request: %w", err)
	}

	if m.client != nil && m.client.IsConnected() {
		subject := SubjectForConfigRequest(gatewayID)
		if err := m.client.Publish(subject, payload); err != nil {
			m.logger.Warn("snapshot request publish failed", slog.String("error", err.Error()))
		} else {
			m.logger.Info("nats reconnected; requested full config snapshot", slog.String("subject", subject))
		}
	}

	return nil
}

// LoadCachedConfig loads and applies cached config from Postgres.
func (m *DegradationManager) LoadCachedConfig(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("load cached config: context is nil")
	}
	if m.configStore == nil {
		return nil
	}

	keys, err := m.configStore.Keys(ctx)
	if err != nil {
		return fmt.Errorf("load cached config: list keys: %w", err)
	}

	versions := make(map[string]int64, len(keys))
	settingsVersion := int64(0)
	for _, key := range keys {
		payload, loadErr := m.configStore.Load(ctx, key)
		if loadErr != nil {
			return fmt.Errorf("load cached config: load key %q: %w", key, loadErr)
		}
		version := extractConfigVersion(key, payload)
		versions[key] = version
		if key == configCacheServerSettingsKey {
			settingsVersion = version
		}
	}

	if m.subscriber != nil {
		if err := m.subscriber.LoadCachedConfig(ctx, m.configStore); err != nil {
			return fmt.Errorf("load cached config: apply cached config: %w", err)
		}
	}

	m.mu.Lock()
	m.lastKnownVersions = versions
	m.hasCachedConfig = len(keys) > 0
	if settingsVersion > 0 {
		m.settingsSyncStatus = "cached"
		m.settingsConfigVersion = settingsVersion
	} else if len(keys) > 0 {
		m.settingsSyncStatus = "cached"
	}
	if m.status == DegradationStatusDisconnected && m.hasCachedConfig {
		m.status = DegradationStatusDegraded
	}
	m.mu.Unlock()

	return nil
}

// Status returns current NATS degradation status.
func (m *DegradationManager) Status() DegradationStatus {
	if m == nil {
		return DegradationStatusDisconnected
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// HasCachedConfig reports whether cached config has been loaded.
func (m *DegradationManager) HasCachedConfig() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hasCachedConfig
}

// EverConnected reports whether NATS has connected at least once.
func (m *DegradationManager) EverConnected() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.everConnected
}

// SettingsSyncStatus reports current server-settings sync status.
func (m *DegradationManager) SettingsSyncStatus() string {
	if m == nil {
		return "unknown"
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settingsSyncStatus
}

// SettingsConfigVersion reports the latest known settings config version.
func (m *DegradationManager) SettingsConfigVersion() int64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settingsConfigVersion
}

type snapshotRequestMessage struct {
	GatewayID         string           `json:"gateway_id"`
	LastKnownVersions map[string]int64 `json:"last_known_versions"`
}

func copyVersionMap(input map[string]int64) map[string]int64 {
	if len(input) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func extractConfigVersion(key string, payload []byte) int64 {
	if key != configCacheServerSettingsKey {
		return 0
	}

	var message ServerSettingsConfigMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return 0
	}
	return message.ConfigVersion
}
