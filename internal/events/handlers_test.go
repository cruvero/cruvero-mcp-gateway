package events

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestPolicyConfigHandlerValidConfig(t *testing.T) {
	t.Parallel()

	engine := &mockPolicyEngine{}
	backend := ratelimit.NewMemoryBackend(time.Minute, 5*time.Minute)
	defer func() { _ = backend.Close() }()
	handler := NewPolicyConfigHandler(nil, backend, nil)
	handler.engine = engine

	payload := PolicyConfigMessage{
		Profiles: []types.PolicyProfile{
			{
				Name:            "default",
				RateLimit:       42,
				RateBurst:       84,
				ToolAllowlist:   []string{"tool.a"},
				ToolDenylist:    []string{"tool.z"},
				EnforcementMode: types.ModeEnforce,
			},
			{
				Name:            "premium",
				RateLimit:       50,
				RateBurst:       100,
				EnforcementMode: types.ModeAudit,
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle policy config: %v", err)
	}
	if len(engine.profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(engine.profiles))
	}

	// After SetDefaults(42, 84), a new key should get limit=42 burst=84.
	allowed, _, _, _ := backend.Allow(context.Background(), ratelimit.LimiterKey{ClientID: "client", Route: "/new"}, 0, 0)
	if !allowed {
		t.Fatal("expected first request to be allowed")
	}
}

func TestPolicyConfigHandlerInvalidConfig(t *testing.T) {
	t.Parallel()

	handler := NewPolicyConfigHandler(nil, nil, nil)

	invalid := []byte(`{"profiles":[{"name":"default","rate_limit":0,"rate_burst":10,"enforcement_mode":"enforce"}]}`)
	if err := handler.Handle(context.Background(), invalid); err == nil {
		t.Fatal("expected validation error for zero rate_limit")
	}

	missingDefault := []byte(`{"profiles":[{"name":"premium","rate_limit":10,"rate_burst":20,"enforcement_mode":"enforce"}]}`)
	if err := handler.Handle(context.Background(), missingDefault); err == nil {
		t.Fatal("expected validation error for missing default profile")
	}
}

func TestServerConfigHandler(t *testing.T) {
	t.Parallel()

	service := &mockServerConfigUpdater{}
	handler := NewServerConfigHandler(service, nil)

	valid := []byte(`{"spiffe_allow_list":["spiffe://cluster-a","spiffe://cluster-b"]}`)
	if err := handler.Handle(context.Background(), valid); err != nil {
		t.Fatalf("handle server config: %v", err)
	}
	if len(service.prefixes) != 2 {
		t.Fatalf("expected 2 prefixes, got %d", len(service.prefixes))
	}

	invalid := []byte(`{"spiffe_allow_list":["http://bad-prefix"]}`)
	if err := handler.Handle(context.Background(), invalid); err == nil {
		t.Fatal("expected validation error for invalid spiffe prefix")
	}
}

func TestServerSettingsConfigHandlerValidVersionedAndInvalid(t *testing.T) {
	t.Parallel()

	service := &mockServerSettingsUpdater{}
	handler := NewServerSettingsConfigHandler(service, nil, nil)

	validV1 := []byte(`{"config_version":1,"servers":[{"server_name":"svc-a","effective_settings":{"max_concurrency":8,"features":{"tracing":true}}}]}`)
	if err := handler.Handle(context.Background(), validV1); err != nil {
		t.Fatalf("handle server settings v1: %v", err)
	}
	if service.version != 1 {
		t.Fatalf("expected version 1 after apply, got %d", service.version)
	}

	validV2 := []byte(`{"config_version":2,"servers":[{"server_name":"svc-a","effective_settings":{"max_concurrency":16}}]}`)
	if err := handler.Handle(context.Background(), validV2); err != nil {
		t.Fatalf("handle server settings v2: %v", err)
	}
	if service.version != 2 {
		t.Fatalf("expected version 2 after apply, got %d", service.version)
	}

	stale := []byte(`{"config_version":1,"servers":[{"server_name":"svc-a","effective_settings":{"max_concurrency":4}}]}`)
	if err := handler.Handle(context.Background(), stale); err == nil {
		t.Fatal("expected stale version error")
	}

	invalidSecret := []byte(`{"config_version":3,"servers":[{"server_name":"svc-a","effective_settings":{"api_token":"secret"}}]}`)
	if err := handler.Handle(context.Background(), invalidSecret); err == nil {
		t.Fatal("expected secret-key validation error")
	}
}

func TestServerRegisteredAckHandler(t *testing.T) {
	t.Parallel()

	updater := &mockServerRegistrationAckUpdater{}
	handler := NewServerRegisteredAckHandler(updater, nil)

	valid := []byte(`{"registration_id":"server-1","lease_epoch":4,"capability_hash":"hash-1","registry_version":"vauto-1","tool_schema_hash":"abc"}`)
	if err := handler.Handle(context.Background(), valid); err != nil {
		t.Fatalf("handle server registration ack: %v", err)
	}
	if updater.registrationID != "server-1" || updater.leaseEpoch != 4 {
		t.Fatalf("unexpected ack target: id=%q lease=%d", updater.registrationID, updater.leaseEpoch)
	}
	if updater.ackedAt.IsZero() {
		t.Fatal("expected acked_at to be populated")
	}

	invalid := []byte(`{"registration_id":"","lease_epoch":0}`)
	if err := handler.Handle(context.Background(), invalid); err == nil {
		t.Fatal("expected validation error")
	}
}

// --- ToolMetadataConfigHandler tests ---

type savingConfigStore struct {
	saved   map[string][]byte
	saveErr error
}

func newSavingConfigStore() *savingConfigStore {
	return &savingConfigStore{saved: make(map[string][]byte)}
}

func (s *savingConfigStore) Save(_ context.Context, key string, value []byte) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved[key] = append([]byte(nil), value...)
	return nil
}

func (s *savingConfigStore) Load(_ context.Context, key string) ([]byte, error) {
	v, ok := s.saved[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func (s *savingConfigStore) Keys(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(s.saved))
	for k := range s.saved {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestToolMetadataConfigHandler_ValidPayload(t *testing.T) {
	t.Parallel()

	store := newSavingConfigStore()
	var received *ToolMetadataConfigMessage
	handler := NewToolMetadataConfigHandler(store, func(msg ToolMetadataConfigMessage) {
		received = &msg
	}, nil)

	payload := ToolMetadataConfigMessage{
		Version: 1,
		Tools: []ToolMetadataEntry{
			{ToolName: "github.create_issue", Category: "github", Tags: []string{"vcs", "issues"}, Priority: 5},
			{ToolName: "slack.send_message", Category: "slack", Summary: "Send message to channel"},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if received == nil {
		t.Fatal("expected onUpdate to be called")
	}
	if received.Version != 1 {
		t.Fatalf("expected version 1, got %d", received.Version)
	}
	if len(received.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(received.Tools))
	}

	// Verify ConfigStore persisted.
	if _, ok := store.saved[configCacheToolMetadataKey]; !ok {
		t.Fatal("expected ConfigStore.Save to be called")
	}
}

func TestToolMetadataConfigHandler_InvalidPayload(t *testing.T) {
	t.Parallel()

	var called bool
	handler := NewToolMetadataConfigHandler(nil, func(_ ToolMetadataConfigMessage) {
		called = true
	}, nil)

	if err := handler.Handle(context.Background(), []byte(`{invalid json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if called {
		t.Fatal("onUpdate should not be called on invalid payload")
	}
}

func TestToolMetadataConfigHandler_EmptyToolsList(t *testing.T) {
	t.Parallel()

	var received *ToolMetadataConfigMessage
	handler := NewToolMetadataConfigHandler(nil, func(msg ToolMetadataConfigMessage) {
		received = &msg
	}, nil)

	body := []byte(`{"version":1,"tools":[]}`)
	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if received == nil {
		t.Fatal("expected onUpdate to be called even with empty tools")
	}
	if len(received.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(received.Tools))
	}
}

func TestToolMetadataConfigHandler_PersistenceError(t *testing.T) {
	t.Parallel()

	store := newSavingConfigStore()
	store.saveErr = fmt.Errorf("db unavailable")

	var received *ToolMetadataConfigMessage
	handler := NewToolMetadataConfigHandler(store, func(msg ToolMetadataConfigMessage) {
		received = &msg
	}, nil)

	body := []byte(`{"version":2,"tools":[{"tool_name":"tool-a","category":"test"}]}`)
	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle should succeed despite persistence error: %v", err)
	}
	if received == nil {
		t.Fatal("onUpdate should still be called when persistence fails")
	}
}

func TestToolMetadataConfigHandler_NilOnUpdate(t *testing.T) {
	t.Parallel()

	handler := NewToolMetadataConfigHandler(nil, nil, nil)

	body := []byte(`{"version":1,"tools":[{"tool_name":"tool-a","category":"test"}]}`)
	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle should not panic with nil onUpdate: %v", err)
	}
}

func TestToolMetadataConfigHandler_SetOnUpdate(t *testing.T) {
	t.Parallel()

	handler := NewToolMetadataConfigHandler(nil, nil, nil)

	var called bool
	handler.SetOnUpdate(func(_ ToolMetadataConfigMessage) {
		called = true
	})

	body := []byte(`{"version":1,"tools":[{"tool_name":"tool-a","category":"test"}]}`)
	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !called {
		t.Fatal("expected late-bound onUpdate to be called")
	}
}

func TestToolMetadataConfigHandler_NilHandler(t *testing.T) {
	t.Parallel()

	var handler *ToolMetadataConfigHandler
	if err := handler.Handle(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestToolMetadataConfigHandler_SetOnUpdateNilReceiver(t *testing.T) {
	t.Parallel()

	var handler *ToolMetadataConfigHandler
	handler.SetOnUpdate(func(_ ToolMetadataConfigMessage) {}) // should not panic
}

func TestToolMetadataConfigHandler_SetOnUpdateReplaysBuffered(t *testing.T) {
	t.Parallel()

	handler := NewToolMetadataConfigHandler(nil, nil, nil)

	// Handle a message before any callback is installed.
	body := []byte(`{"version":42,"tools":[{"tool_name":"tool-a","category":"test"}]}`)
	if err := handler.Handle(context.Background(), body); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// Now install the callback — it should receive the buffered message.
	var received *ToolMetadataConfigMessage
	handler.SetOnUpdate(func(msg ToolMetadataConfigMessage) {
		received = &msg
	})

	if received == nil {
		t.Fatal("expected buffered message to be replayed on SetOnUpdate")
	}
	if received.Version != 42 {
		t.Fatalf("expected version 42, got %d", received.Version)
	}
	if len(received.Tools) != 1 || received.Tools[0].ToolName != "tool-a" {
		t.Fatal("expected replayed message to contain the original tool")
	}
}

func TestToolMetadataConfigHandler_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	handler := NewToolMetadataConfigHandler(nil, nil, nil)
	handler.SetOnUpdate(func(_ ToolMetadataConfigMessage) {})

	body := []byte(`{"version":1,"tools":[{"tool_name":"tool-a","category":"test"}]}`)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			_ = handler.Handle(context.Background(), body)
		}
	}()

	for range 100 {
		handler.SetOnUpdate(func(_ ToolMetadataConfigMessage) {})
	}
	<-done
}

type mockPolicyEngine struct {
	profiles map[string]*types.PolicyProfile
}

func (m *mockPolicyEngine) ReplaceProfiles(profiles map[string]*types.PolicyProfile) {
	m.profiles = profiles
}

type mockServerConfigUpdater struct {
	prefixes []string
}

func (m *mockServerConfigUpdater) UpdateSPIFFEAllowList(prefixes []string) {
	m.prefixes = append([]string(nil), prefixes...)
}

type mockServerSettingsUpdater struct {
	version  int64
	settings map[string]map[string]any
}

func (m *mockServerSettingsUpdater) UpdateEffectiveSettings(configVersion int64, settingsByServer map[string]map[string]any) error {
	if configVersion <= 0 {
		return fmt.Errorf("version must be positive")
	}
	if configVersion < m.version {
		return fmt.Errorf("stale version")
	}
	m.version = configVersion
	m.settings = settingsByServer
	return nil
}

type mockServerRegistrationAckUpdater struct {
	registrationID  string
	leaseEpoch      int64
	capabilityHash  string
	registryVersion string
	toolSchemaHash  string
	ackedAt         time.Time
}

func (m *mockServerRegistrationAckUpdater) AcknowledgeServerRegistration(
	_ context.Context,
	registrationID string,
	leaseEpoch int64,
	capabilityHash string,
	registryVersion string,
	toolSchemaHash string,
	ackedAt time.Time,
) error {
	m.registrationID = registrationID
	m.leaseEpoch = leaseEpoch
	m.capabilityHash = capabilityHash
	m.registryVersion = registryVersion
	m.toolSchemaHash = toolSchemaHash
	m.ackedAt = ackedAt
	return nil
}
