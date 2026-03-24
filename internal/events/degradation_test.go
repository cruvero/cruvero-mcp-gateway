package events

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestDegradationManagerOnDisconnectSetsState(t *testing.T) {
	t.Parallel()

	manager := NewDegradationManager(nil, nil, nil, nil)
	if manager.Status() != DegradationStatusDisconnected {
		t.Fatalf("expected initial disconnected status, got %q", manager.Status())
	}

	manager.OnDisconnect()
	if manager.Status() != DegradationStatusDegraded {
		t.Fatalf("expected degraded status, got %q", manager.Status())
	}
}

func TestDegradationManagerOnReconnectPublishesSnapshotRequest(t *testing.T) {
	t.Parallel()

	srv := runNATSServer(t, 0)
	defer srv.Shutdown()

	client, err := NewClient(natsServerURL(t, srv), "gw-degrade")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	subject := SubjectForConfigRequest("gw-degrade")
	messages := make(chan *nats.Msg, 1)
	sub, err := client.Subscribe(subject, func(msg *nats.Msg) {
		messages <- msg
	})
	if err != nil {
		t.Fatalf("subscribe request subject: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	manager := NewDegradationManager(client, nil, nil, nil)
	manager.mu.Lock()
	manager.lastKnownVersions[configCacheServerSettingsKey] = 3
	manager.mu.Unlock()

	if err := manager.OnReconnect(context.Background()); err != nil {
		t.Fatalf("on reconnect: %v", err)
	}
	if manager.Status() != DegradationStatusConnected {
		t.Fatalf("expected connected status, got %q", manager.Status())
	}
	if !manager.EverConnected() {
		t.Fatal("expected ever connected state")
	}

	select {
	case msg := <-messages:
		var request snapshotRequestMessage
		if err := json.Unmarshal(msg.Data, &request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.GatewayID != "gw-degrade" {
			t.Fatalf("expected gateway id gw-degrade, got %q", request.GatewayID)
		}
		if request.LastKnownVersions[configCacheServerSettingsKey] != 3 {
			t.Fatalf("expected settings version 3, got %d", request.LastKnownVersions[configCacheServerSettingsKey])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for snapshot request")
	}
}

func TestDegradationManagerLoadCachedConfig(t *testing.T) {
	t.Parallel()

	srv := runNATSServer(t, 0)
	defer srv.Shutdown()

	client, err := NewClient(natsServerURL(t, srv), "gw-cache")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	var policyCalls atomic.Int32
	subscriber := NewSubscriber(client, nil)
	subscriber.RegisterGatewaySubjects(
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error {
			policyCalls.Add(1)
			return nil
		}),
		nil,
		nil,
		nil,
	)

	store := &mockConfigStore{
		keys: []string{configCachePolicyKey, configCacheServerSettingsKey},
		values: map[string][]byte{
			configCachePolicyKey:         []byte(`{"profiles":[{"name":"default","rate_limit":10,"rate_burst":20,"enforcement_mode":"enforce"}]}`),
			configCacheServerSettingsKey: []byte(`{"config_version":7,"servers":[{"server_name":"svc-a","effective_settings":{"max_concurrency":8}}]}`),
		},
	}

	manager := NewDegradationManager(client, store, subscriber, nil)
	if err := manager.LoadCachedConfig(context.Background()); err != nil {
		t.Fatalf("load cached config: %v", err)
	}
	if !manager.HasCachedConfig() {
		t.Fatal("expected cached config state")
	}
	if manager.SettingsSyncStatus() != "cached" {
		t.Fatalf("expected cached settings sync status, got %q", manager.SettingsSyncStatus())
	}
	if manager.SettingsConfigVersion() != 7 {
		t.Fatalf("expected settings config version 7, got %d", manager.SettingsConfigVersion())
	}
	if policyCalls.Load() != 1 {
		t.Fatalf("expected policy handler to run once, got %d", policyCalls.Load())
	}
}

func TestDegradationManager_LoadCachedConfig_WithStore(t *testing.T) {
	t.Parallel()

	store := &mockConfigStore{
		keys: []string{configCacheServerSettingsKey},
		values: map[string][]byte{
			configCacheServerSettingsKey: []byte(`{"config_version":5,"servers":[]}`),
		},
	}

	manager := NewDegradationManager(nil, store, nil, nil)
	if err := manager.LoadCachedConfig(context.Background()); err != nil {
		t.Fatalf("load cached config: %v", err)
	}
	if manager.Status() != DegradationStatusDegraded {
		t.Fatalf("expected degraded status after loading cache, got %q", manager.Status())
	}
	if !manager.HasCachedConfig() {
		t.Fatal("expected has cached config to be true")
	}
}

func TestDegradationManager_LoadCachedConfig_NilStore(t *testing.T) {
	t.Parallel()

	manager := NewDegradationManager(nil, nil, nil, nil)
	if err := manager.LoadCachedConfig(context.Background()); err != nil {
		t.Fatalf("expected no error with nil store, got %v", err)
	}
	if manager.HasCachedConfig() {
		t.Fatal("expected no cached config with nil store")
	}
}

func TestDegradationManager_LoadCachedConfig_EmptyCache(t *testing.T) {
	t.Parallel()

	store := &mockConfigStore{
		keys:   []string{},
		values: map[string][]byte{},
	}

	manager := NewDegradationManager(nil, store, nil, nil)
	if err := manager.LoadCachedConfig(context.Background()); err != nil {
		t.Fatalf("expected no error with empty cache, got %v", err)
	}
	if manager.Status() != DegradationStatusDisconnected {
		t.Fatalf("expected disconnected status with empty cache, got %q", manager.Status())
	}
	if manager.HasCachedConfig() {
		t.Fatal("expected no cached config with empty keys")
	}
}

func TestDegradationManager_LoadCachedConfig_StoreError(t *testing.T) {
	t.Parallel()

	store := &mockConfigStore{
		keysErr: errors.New("db connection lost"),
	}

	manager := NewDegradationManager(nil, store, nil, nil)
	err := manager.LoadCachedConfig(context.Background())
	if err == nil {
		t.Fatal("expected error when store returns error")
	}
	if !errors.Is(err, store.keysErr) {
		t.Fatalf("expected wrapped store error, got %v", err)
	}
}

func TestDegradationManager_LoadCachedConfig_NilReceiver(t *testing.T) {
	t.Parallel()

	var manager *DegradationManager
	if err := manager.LoadCachedConfig(context.Background()); err != nil {
		t.Fatalf("expected no error on nil receiver, got %v", err)
	}
}
