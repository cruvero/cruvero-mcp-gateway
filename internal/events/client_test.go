package events

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func TestClientPublishSubscribe(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(
		fmt.Sprintf("nats://127.0.0.1:%d", port),
		"gw-1",
		WithReconnectWait(50*time.Millisecond),
		WithMaxReconnects(10),
	)
	if err != nil {
		t.Fatalf("new nats client: %v", err)
	}
	defer func() { _ = client.Close() }()

	received := make(chan []byte, 1)
	_, err = client.Subscribe("test.events", func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := client.Publish("test.events", []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case got := <-received:
		if string(got) != "hello" {
			t.Fatalf("expected hello payload, got %q", string(got))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestClientDisconnectAndReconnectHandlers(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(
		fmt.Sprintf("nats://127.0.0.1:%d", port),
		"gw-1",
		WithReconnectWait(50*time.Millisecond),
		WithMaxReconnects(20),
	)
	if err != nil {
		t.Fatalf("new nats client: %v", err)
	}
	defer func() { _ = client.Close() }()

	var disconnectCalls atomic.Int32
	var reconnectCalls atomic.Int32
	client.SetDisconnectHandler(func() {
		disconnectCalls.Add(1)
	})
	client.SetReconnectHandler(func() {
		reconnectCalls.Add(1)
	})

	srv.Shutdown()

	waitFor(t, 3*time.Second, func() bool {
		return !client.IsConnected() && disconnectCalls.Load() > 0
	})

	srv = runNATSServer(t, port)
	defer srv.Shutdown()

	waitFor(t, 4*time.Second, func() bool {
		return client.IsConnected() && reconnectCalls.Load() > 0
	})
}

func TestClientClose(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-1")
	if err != nil {
		t.Fatalf("new nats client: %v", err)
	}
	if !client.IsConnected() {
		t.Fatal("expected connected client")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if client.IsConnected() {
		t.Fatal("expected disconnected state after close")
	}
}

func runNATSServer(t *testing.T, port int) *natsserver.Server {
	t.Helper()

	opts := &natsserver.Options{
		Host:   "127.0.0.1",
		Port:   port,
		NoLog:  true,
		NoSigs: true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("new nats server: %v", err)
	}

	go srv.Start()
	if !srv.ReadyForConnections(3 * time.Second) {
		srv.Shutdown()
		t.Fatal("nats server did not become ready in time")
	}
	return srv
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not satisfied before timeout")
}

