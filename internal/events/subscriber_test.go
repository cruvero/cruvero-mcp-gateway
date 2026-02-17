package events

import (
	"context"
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
	if err := subscriber.Start(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ConfigHandlerFunc adapts a function into a ConfigHandler.
type ConfigHandlerFunc func(ctx context.Context, data []byte) error

// Handle implements ConfigHandler.
func (f ConfigHandlerFunc) Handle(ctx context.Context, data []byte) error {
	return f(ctx, data)
}
