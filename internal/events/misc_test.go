package events

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"

	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/nats-io/nats.go"
)

func TestWithTLSOptionSetsConfig(t *testing.T) {
	t.Parallel()

	cfg := &clientConfig{}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	WithTLS(tlsConfig)(cfg)
	if cfg.tlsConfig != tlsConfig {
		t.Fatal("expected tls config to be set")
	}
}

func TestClientMethodsHandleNil(t *testing.T) {
	t.Parallel()

	var client *Client
	if err := client.Publish("subject", []byte("x")); err == nil {
		t.Fatal("expected publish error for nil client")
	}
	if _, err := client.Subscribe("subject", func(msg *nats.Msg) {}); err == nil {
		t.Fatal("expected subscribe error for nil client")
	}
	if client.IsConnected() {
		t.Fatal("expected nil client disconnected")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("expected nil close error, got %v", err)
	}
	if got := client.GatewayID(); got != "" {
		t.Fatalf("expected empty gateway id, got %q", got)
	}
	client.SetDisconnectHandler(func() {})
	client.SetReconnectHandler(func() {})
}

func TestSubjectHelpersFallbackGateway(t *testing.T) {
	t.Parallel()

	if got := SubjectForEvent("", EventServerRegistered); got != "mcpgw.unknown.events.server.registered" {
		t.Fatalf("unexpected fallback event subject: %q", got)
	}
	if got := SubjectForConfig("", ConfigScopePolicy); got != "mcpgw.unknown.config.policy" {
		t.Fatalf("unexpected fallback config subject: %q", got)
	}
	if got := SubjectForConfigRequest(""); got != "mcpgw.unknown.config.request" {
		t.Fatalf("unexpected fallback request subject: %q", got)
	}
}

func TestCacheKeyToSubject(t *testing.T) {
	t.Parallel()

	gatewayID := "gw-map"
	if got := cacheKeyToSubject(gatewayID, configCachePolicyKey); got != SubjectForConfig(gatewayID, ConfigScopePolicy) {
		t.Fatalf("unexpected policy mapping: %q", got)
	}
	if got := cacheKeyToSubject(gatewayID, configCacheServersKey); got != SubjectForConfig(gatewayID, ConfigScopeServers) {
		t.Fatalf("unexpected servers mapping: %q", got)
	}
	if got := cacheKeyToSubject(gatewayID, configCacheServerSettingsKey); got != SubjectForConfig(gatewayID, ConfigScopeServerSettings) {
		t.Fatalf("unexpected server settings mapping: %q", got)
	}
	if got := cacheKeyToSubject(gatewayID, configCacheAuthKey); got != SubjectForConfig(gatewayID, ConfigScopeAuth) {
		t.Fatalf("unexpected auth mapping: %q", got)
	}
	if got := cacheKeyToSubject(gatewayID, "unknown.key"); got != "" {
		t.Fatalf("expected unknown key mapping to empty subject, got %q", got)
	}
}

func TestHandlersSetConfigStoreAndPersist(t *testing.T) {
	t.Parallel()

	store := &countingConfigStore{values: make(map[string][]byte)}

	policyHandler := NewPolicyConfigHandler(nil, nil, nil)
	policyHandler.SetConfigStore(store)
	policyPayload := []byte(`{"profiles":[{"name":"default","rate_limit":10,"rate_burst":20,"enforcement_mode":"enforce"}]}`)
	if err := policyHandler.Handle(context.Background(), policyPayload); err != nil {
		t.Fatalf("handle policy config: %v", err)
	}
	if store.saveCount == 0 {
		t.Fatal("expected policy config to be persisted")
	}

	serverHandler := NewServerConfigHandler(nil, nil)
	serverHandler.SetConfigStore(store)
	if err := serverHandler.Handle(context.Background(), []byte(`{"spiffe_allow_list":["spiffe://cluster-a"]}`)); err != nil {
		t.Fatalf("handle server config: %v", err)
	}

	authHandler := NewAuthConfigHandler(nil)
	authHandler.SetConfigStore(store)
	if err := authHandler.Handle(context.Background(), []byte(`{"mode":"jwt"}`)); err != nil {
		t.Fatalf("handle auth config: %v", err)
	}
	if err := authHandler.Handle(context.Background(), []byte(`{"mode":`)); err == nil {
		t.Fatal("expected invalid auth payload error")
	}
}

func TestValidateNonSecretSettingAndClone(t *testing.T) {
	t.Parallel()

	if err := validateNonSecretSetting("", "x"); err == nil {
		t.Fatal("expected empty key validation error")
	}
	if err := validateNonSecretSetting("api_token", "x"); err == nil {
		t.Fatal("expected secret-like key validation error")
	}
	if err := validateNonSecretSetting("max", []any{map[string]any{"nested": true}}); err != nil {
		t.Fatalf("expected nested values to validate, got %v", err)
	}
	if err := validateNonSecretSetting("max", make(chan int)); err == nil {
		t.Fatal("expected unsupported type validation error")
	}

	cloned := cloneSettingsMap(map[string]any{
		"max": float64(8),
		"nested": map[string]any{
			"enabled": true,
		},
		"list": []any{map[string]any{"name": "a"}},
	})
	if _, ok := cloned["nested"].(map[string]any); !ok {
		t.Fatalf("expected nested map clone, got %#v", cloned["nested"])
	}
}

func TestPersistenceStoreNilErrors(t *testing.T) {
	t.Parallel()

	store := NewPostgresConfigStore(nil)
	if err := store.Save(context.Background(), "x", []byte("y")); err == nil {
		t.Fatal("expected save error for nil db")
	}
	if _, err := store.Load(context.Background(), "x"); err == nil {
		t.Fatal("expected load error for nil db")
	}
	if _, err := store.Keys(context.Background()); err == nil {
		t.Fatal("expected keys error for nil db")
	}
}

func TestNormalizePolicyProfileValidation(t *testing.T) {
	t.Parallel()

	if _, err := normalizePolicyProfile(types.PolicyProfile{}); err == nil {
		t.Fatal("expected missing name error")
	}
	if _, err := normalizePolicyProfile(types.PolicyProfile{Name: "default", RateLimit: 1, RateBurst: 1, EnforcementMode: "invalid"}); err == nil {
		t.Fatal("expected invalid enforcement mode error")
	}

	profile, err := normalizePolicyProfile(types.PolicyProfile{
		Name:            "default",
		RateLimit:       10,
		RateBurst:       20,
		ToolAllowlist:   []string{"a", "a", ""},
		ToolDenylist:    []string{"b", " ", "b"},
		EnforcementMode: types.ModeEnforce,
	})
	if err != nil {
		t.Fatalf("normalize policy profile: %v", err)
	}
	if len(profile.ToolAllowlist) != 1 || len(profile.ToolDenylist) != 1 {
		t.Fatalf("expected deduped allow/deny lists, got %+v", profile)
	}
}

func TestSubscriberLoadCachedConfigNilStore(t *testing.T) {
	t.Parallel()

	srv := runNATSServer(t, 0)
	defer srv.Shutdown()

	client, err := NewClient(natsServerURL(t, srv), "gw-nil-store")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	subscriber := NewSubscriber(client, nil)
	if err := subscriber.LoadCachedConfig(context.Background(), nil); err != nil {
		t.Fatalf("expected nil store to be no-op, got %v", err)
	}
}

type countingConfigStore struct {
	saveCount int
	keys      []string
	values    map[string][]byte
}

func (s *countingConfigStore) Save(ctx context.Context, key string, value []byte) error {
	s.saveCount++
	if s.values == nil {
		s.values = make(map[string][]byte)
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *countingConfigStore) Load(ctx context.Context, key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, fmt.Errorf("missing key: %s", key)
	}
	return append([]byte(nil), value...), nil
}

func (s *countingConfigStore) Keys(ctx context.Context) ([]string, error) {
	return append([]string(nil), s.keys...), nil
}
