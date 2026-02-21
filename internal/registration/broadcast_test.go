package registration

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func TestNATSBroadcasterPublishSubscribe(t *testing.T) {
	t.Parallel()

	conn := startNATSForBroadcast(t)
	b := NewNATSBroadcaster(conn, nil)
	defer func() { _ = b.Close() }()

	received := make(chan []byte, 1)
	if err := b.Subscribe("test.subject", func(data []byte) {
		received <- data
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Give subscription time to register.
	time.Sleep(50 * time.Millisecond)

	payload := []byte(`{"event_type":"registered","server_id":"srv-1"}`)
	if err := b.Publish("test.subject", payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case data := <-received:
		var evt RegistrationEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if evt.EventType != "registered" || evt.ServerID != "srv-1" {
			t.Fatalf("unexpected event: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestNATSBroadcasterMultipleSubjects(t *testing.T) {
	t.Parallel()

	conn := startNATSForBroadcast(t)
	b := NewNATSBroadcaster(conn, nil)
	defer func() { _ = b.Close() }()

	ch1 := make(chan []byte, 1)
	ch2 := make(chan []byte, 1)

	if err := b.Subscribe("subject.one", func(data []byte) { ch1 <- data }); err != nil {
		t.Fatalf("subscribe one: %v", err)
	}
	if err := b.Subscribe("subject.two", func(data []byte) { ch2 <- data }); err != nil {
		t.Fatalf("subscribe two: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if err := b.Publish("subject.one", []byte("msg1")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := b.Publish("subject.two", []byte("msg2")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case data := <-ch1:
		if string(data) != "msg1" {
			t.Fatalf("expected msg1, got %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout on subject.one")
	}

	select {
	case data := <-ch2:
		if string(data) != "msg2" {
			t.Fatalf("expected msg2, got %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout on subject.two")
	}
}

func TestNATSBroadcasterCloseUnsubscribes(t *testing.T) {
	t.Parallel()

	conn := startNATSForBroadcast(t)
	b := NewNATSBroadcaster(conn, nil)

	if err := b.Subscribe("test.close", func(_ []byte) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestDragonflyBroadcasterPublishSubscribe(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	b := NewDragonflyBroadcaster(client, nil)
	defer func() { _ = b.Close() }()

	received := make(chan []byte, 1)
	if err := b.Subscribe("test.subject", func(data []byte) {
		received <- data
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Give subscription time to register.
	time.Sleep(100 * time.Millisecond)

	payload := []byte(`{"tool_name":"exec","risk_level":"destructive"}`)
	if err := b.Publish("test.subject", payload); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case data := <-received:
		var evt ClassificationEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if evt.ToolName != "exec" || evt.RiskLevel != "destructive" {
			t.Fatalf("unexpected event: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestNoopBroadcasterNoPanic(t *testing.T) {
	t.Parallel()

	b := NewNoopBroadcaster()
	if err := b.Publish("test", []byte("data")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := b.Subscribe("test", func(_ []byte) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestEventMarshaling(t *testing.T) {
	t.Parallel()

	regEvt := NewRegistrationEvent("registered", "srv-1")
	data, err := json.Marshal(regEvt)
	if err != nil {
		t.Fatalf("marshal registration event: %v", err)
	}
	var decoded RegistrationEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.EventType != "registered" || decoded.ServerID != "srv-1" || decoded.Timestamp <= 0 {
		t.Fatalf("unexpected decoded event: %+v", decoded)
	}

	classEvt := NewClassificationEvent("exec", "destructive", "admin")
	data, err = json.Marshal(classEvt)
	if err != nil {
		t.Fatalf("marshal classification event: %v", err)
	}
	var decodedClass ClassificationEvent
	if err := json.Unmarshal(data, &decodedClass); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decodedClass.ToolName != "exec" || decodedClass.RiskLevel != "destructive" || decodedClass.UpdatedBy != "admin" {
		t.Fatalf("unexpected decoded event: %+v", decodedClass)
	}
}

func startNATSForBroadcast(t *testing.T) *nats.Conn {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

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
		t.Fatal("nats server not ready")
	}
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)

	return nc
}
