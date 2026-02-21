package ratelimit

import (
	"context"
	"net"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startNATSWithJetStream(t *testing.T) (*natsserver.Server, nats.JetStreamContext) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      port,
		NoLog:     true,
		NoSigs:    true,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(3 * time.Second) {
		srv.Shutdown()
		t.Fatal("nats server not ready")
	}

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		srv.Shutdown()
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(func() {
		nc.Close()
		srv.Shutdown()
	})

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	return srv, js
}

func TestNATSBackendAllowed(t *testing.T) {
	t.Parallel()
	_, js := startNATSWithJetStream(t)

	nb, err := NewNATSBackend(js)
	if err != nil {
		t.Fatalf("new nats backend: %v", err)
	}

	key := LimiterKey{ClientID: "client-a", Route: "/mcp"}
	allowed, remaining, retryAfter, err := nb.Allow(context.Background(), key, 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected request to be allowed")
	}
	if remaining != 4 {
		t.Fatalf("expected 4 remaining, got %d", remaining)
	}
	if retryAfter != 0 {
		t.Fatalf("expected zero retryAfter, got %v", retryAfter)
	}
}

func TestNATSBackendBurstExhausted(t *testing.T) {
	t.Parallel()
	_, js := startNATSWithJetStream(t)

	nb, err := NewNATSBackend(js)
	if err != nil {
		t.Fatalf("new nats backend: %v", err)
	}

	key := LimiterKey{ClientID: "client-burst", Route: "/mcp"}
	for i := 0; i < 3; i++ {
		allowed, _, _, err := nb.Allow(context.Background(), key, 10, 3)
		if err != nil {
			t.Fatalf("request %d error: %v", i, err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// Fourth should be rejected.
	allowed, remaining, retryAfter, err := nb.Allow(context.Background(), key, 10, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("expected request to be rejected")
	}
	if remaining != 0 {
		t.Fatalf("expected 0 remaining, got %d", remaining)
	}
	if retryAfter <= 0 {
		t.Fatal("expected positive retryAfter")
	}
}

func TestNATSBackendBucketAutoCreation(t *testing.T) {
	t.Parallel()
	_, js := startNATSWithJetStream(t)

	// The bucket should not exist yet.
	nb, err := NewNATSBackend(js)
	if err != nil {
		t.Fatalf("expected auto-creation, got error: %v", err)
	}

	key := LimiterKey{ClientID: "auto-create", Route: "/mcp"}
	allowed, _, _, err := nb.Allow(context.Background(), key, 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("expected request to be allowed")
	}
}

func TestNATSBackendNilSafety(t *testing.T) {
	t.Parallel()

	var nb *NATSBackend
	allowed, _, _, err := nb.Allow(context.Background(), LimiterKey{}, 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("nil backend should allow all")
	}
	_ = nb.Close()
}
