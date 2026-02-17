package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriberRegisterGatewaySubjects(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-subjects")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	subscriber := NewSubscriber(client, nil)
	subscriber.RegisterGatewaySubjects(
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error { return nil }),
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error { return nil }),
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error { return nil }),
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error { return nil }),
	)

	subscriber.mu.RLock()
	defer subscriber.mu.RUnlock()
	if len(subscriber.handlers) != 4 {
		t.Fatalf("expected 4 handlers, got %d", len(subscriber.handlers))
	}

	expected := []string{
		SubjectForConfig("gw-subjects", ConfigScopePolicy),
		SubjectForConfig("gw-subjects", ConfigScopeServers),
		SubjectForConfig("gw-subjects", ConfigScopeServerSettings),
		SubjectForConfig("gw-subjects", ConfigScopeAuth),
	}
	for _, subject := range expected {
		if _, ok := subscriber.handlers[subject]; !ok {
			t.Fatalf("expected subject handler registration for %q", subject)
		}
	}
}

func TestSubscriberRoutesMessageToRegisteredHandler(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-route")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	subject := SubjectForConfig("gw-route", ConfigScopePolicy)
	received := make(chan []byte, 1)
	subscriber := NewSubscriber(client, nil)
	subscriber.RegisterHandler(subject, ConfigHandlerFunc(func(ctx context.Context, data []byte) error {
		received <- data
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := subscriber.Start(ctx); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}
	defer func() { _ = subscriber.Stop() }()

	payload := []byte(`{"profiles":[]}`)
	if err := client.Publish(subject, payload); err != nil {
		t.Fatalf("publish test message: %v", err)
	}

	select {
	case got := <-received:
		if string(got) != string(payload) {
			t.Fatalf("unexpected payload: %q", string(got))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for routed message")
	}
}

func TestSubscriberStartStopLifecycle(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-life")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	subscriber := NewSubscriber(client, nil)
	var calls atomic.Int32
	subject := SubjectForConfig("gw-life", ConfigScopeServers)
	subscriber.RegisterHandler(subject, ConfigHandlerFunc(func(ctx context.Context, data []byte) error {
		calls.Add(1)
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := subscriber.Start(ctx); err != nil {
		t.Fatalf("start subscriber: %v", err)
	}
	if len(subscriber.subscriptions) != 1 {
		t.Fatalf("expected 1 subscription, got %d", len(subscriber.subscriptions))
	}

	if err := client.Publish(subject, []byte(`{"spiffe_allow_list":["spiffe://x"]}`)); err != nil {
		t.Fatalf("publish message: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return calls.Load() == 1 })

	if err := subscriber.Stop(); err != nil {
		t.Fatalf("stop subscriber: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
}

func TestSubscriberStartErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if err := (*Subscriber)(nil).Start(ctx); err == nil {
		t.Fatal("expected error for nil subscriber")
	}

	subscriber := NewSubscriber(nil, nil)
	if err := subscriber.Start(ctx); err == nil {
		t.Fatal("expected error for nil client")
	}

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()
	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-errors")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	subscriber = NewSubscriber(client, nil)
	if err := subscriber.Start(nilContext()); err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestSubscriberLoadCachedConfig(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-cache")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	var policyCalls atomic.Int32
	var serverCalls atomic.Int32
	subscriber := NewSubscriber(client, nil)
	subscriber.RegisterGatewaySubjects(
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error {
			policyCalls.Add(1)
			return nil
		}),
		ConfigHandlerFunc(func(ctx context.Context, data []byte) error {
			serverCalls.Add(1)
			return nil
		}),
		nil,
		nil,
	)

	store := &mockConfigStore{
		keys: []string{configCachePolicyKey, configCacheServersKey},
		values: map[string][]byte{
			configCachePolicyKey:  []byte(`{"profiles":[{"name":"default","rate_limit":10,"rate_burst":20,"enforcement_mode":"enforce"}]}`),
			configCacheServersKey: []byte(`{"spiffe_allow_list":["spiffe://cluster-a"]}`),
		},
	}
	if err := subscriber.LoadCachedConfig(context.Background(), store); err != nil {
		t.Fatalf("load cached config: %v", err)
	}
	if policyCalls.Load() != 1 || serverCalls.Load() != 1 {
		t.Fatalf("expected both handlers to be called once, got policy=%d servers=%d", policyCalls.Load(), serverCalls.Load())
	}
}

func TestSubscriberLoadCachedConfigErrors(t *testing.T) {
	t.Parallel()

	if err := (*Subscriber)(nil).LoadCachedConfig(context.Background(), &mockConfigStore{}); err == nil {
		t.Fatal("expected error for nil subscriber")
	}

	subscriber := NewSubscriber(nil, nil)
	if err := subscriber.LoadCachedConfig(context.Background(), &mockConfigStore{}); err == nil {
		t.Fatal("expected error for nil client")
	}

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-cache-errors")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer func() { _ = client.Close() }()

	subscriber = NewSubscriber(client, nil)
	if err := subscriber.LoadCachedConfig(nilContext(), &mockConfigStore{}); err == nil {
		t.Fatal("expected error for nil context")
	}

	errStore := &mockConfigStore{keysErr: errors.New("boom")}
	if err := subscriber.LoadCachedConfig(context.Background(), errStore); err == nil {
		t.Fatal("expected key loading error")
	}

	store := &mockConfigStore{
		keys:     []string{configCachePolicyKey},
		loadErrs: map[string]error{configCachePolicyKey: sql.ErrNoRows},
	}
	if err := subscriber.LoadCachedConfig(context.Background(), store); err == nil {
		t.Fatal("expected key load error")
	}
}

// ConfigHandlerFunc adapts a function into a ConfigHandler.
type ConfigHandlerFunc func(ctx context.Context, data []byte) error

// Handle implements ConfigHandler.
func (f ConfigHandlerFunc) Handle(ctx context.Context, data []byte) error {
	return f(ctx, data)
}

type mockConfigStore struct {
	keys     []string
	values   map[string][]byte
	keysErr  error
	loadErrs map[string]error
}

func (m *mockConfigStore) Save(ctx context.Context, key string, value []byte) error {
	return nil
}

func (m *mockConfigStore) Load(ctx context.Context, key string) ([]byte, error) {
	if m.loadErrs != nil {
		if err, ok := m.loadErrs[key]; ok {
			return nil, err
		}
	}
	if m.values == nil {
		return nil, sql.ErrNoRows
	}
	value, ok := m.values[key]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return value, nil
}

func (m *mockConfigStore) Keys(ctx context.Context) ([]string, error) {
	_ = ctx
	if m.keysErr != nil {
		return nil, m.keysErr
	}
	return append([]string(nil), m.keys...), nil
}

func nilContext() context.Context {
	return nil
}
