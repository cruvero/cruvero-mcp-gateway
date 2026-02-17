package events

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/ratelimit"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestPolicyConfigHandlerValidConfig(t *testing.T) {
	t.Parallel()

	engine := &mockPolicyEngine{}
	limiterStore := ratelimit.NewLimiterStore(1, 1)
	handler := NewPolicyConfigHandler(nil, limiterStore, nil)
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

	limiter := limiterStore.GetOrCreate(ratelimit.LimiterKey{ClientID: "client", Route: "/new"}, nil)
	if got := float64(limiter.Limit()); got != 42 {
		t.Fatalf("expected default rate 42, got %v", got)
	}
	if got := limiter.Burst(); got != 84 {
		t.Fatalf("expected default burst 84, got %d", got)
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
