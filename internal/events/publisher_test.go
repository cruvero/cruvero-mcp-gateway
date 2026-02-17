package events

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
	"github.com/nats-io/nats.go"
)

func TestPublisherServerRegistered(t *testing.T) {
	t.Parallel()

	publisher, subject, messages, cleanup := newPublisherFixture(t, EventServerRegistered)
	defer cleanup()

	err := publisher.PublishServerRegistered(context.Background(), types.ServerRecord{
		ID:       "server-1",
		Name:     "alpha",
		SPIFFEID: "spiffe://example.org/ns/default/sa/alpha",
		Host:     "alpha.default.svc",
		Port:     8443,
		Capabilities: types.Capability{
			Tools: []string{"tool.echo"},
		},
	})
	if err != nil {
		t.Fatalf("publish server registered: %v", err)
	}

	envelope := expectEventEnvelope(t, messages, subject)
	if envelope.EventType != EventServerRegistered {
		t.Fatalf("expected event type %q, got %q", EventServerRegistered, envelope.EventType)
	}
	if envelope.GatewayID != "gw-1" {
		t.Fatalf("expected gateway id gw-1, got %q", envelope.GatewayID)
	}

	var payload ServerRegisteredPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Endpoint != "alpha.default.svc:8443" {
		t.Fatalf("expected endpoint alpha.default.svc:8443, got %q", payload.Endpoint)
	}
}

func TestPublisherServerDeregistered(t *testing.T) {
	t.Parallel()

	publisher, subject, messages, cleanup := newPublisherFixture(t, EventServerDeregistered)
	defer cleanup()

	err := publisher.PublishServerDeregistered(context.Background(), "server-1", "alpha", "manual")
	if err != nil {
		t.Fatalf("publish server deregistered: %v", err)
	}

	envelope := expectEventEnvelope(t, messages, subject)
	var payload ServerDeregisteredPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Reason != "manual" {
		t.Fatalf("expected reason manual, got %q", payload.Reason)
	}
}

func TestPublisherServerHealthChanged(t *testing.T) {
	t.Parallel()

	publisher, subject, messages, cleanup := newPublisherFixture(t, EventServerHealthChanged)
	defer cleanup()

	err := publisher.PublishServerHealthChanged(context.Background(), "server-1", "alpha", types.StatusActive, types.StatusStale)
	if err != nil {
		t.Fatalf("publish server health changed: %v", err)
	}

	envelope := expectEventEnvelope(t, messages, subject)
	var payload ServerHealthChangedPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.OldStatus != types.StatusActive || payload.NewStatus != types.StatusStale {
		t.Fatalf("unexpected status transition payload: %+v", payload)
	}
}

func TestPublisherPolicyViolated(t *testing.T) {
	t.Parallel()

	publisher, subject, messages, cleanup := newPublisherFixture(t, EventPolicyViolated)
	defer cleanup()

	err := publisher.PublishPolicyViolated(context.Background(), "client-1", "danger.tool", []string{"denylist"}, "denied")
	if err != nil {
		t.Fatalf("publish policy violated: %v", err)
	}

	envelope := expectEventEnvelope(t, messages, subject)
	var payload PolicyViolatedPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Decision != "denied" {
		t.Fatalf("expected denied decision, got %q", payload.Decision)
	}
}

func TestPublisherDisconnectedNoError(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	srv := runNATSServer(t, port)
	defer srv.Shutdown()

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-1")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_ = client.Close()

	publisher := NewPublisher(client, "gw-1", nil)
	if err := publisher.PublishServerDeregistered(context.Background(), "server-1", "alpha", "manual"); err != nil {
		t.Fatalf("expected no error while disconnected, got %v", err)
	}
}

func newPublisherFixture(
	t *testing.T,
	eventType string,
) (*Publisher, string, <-chan *nats.Msg, func()) {
	t.Helper()

	port := freePort(t)
	srv := runNATSServer(t, port)

	client, err := NewClient(fmt.Sprintf("nats://127.0.0.1:%d", port), "gw-1")
	if err != nil {
		srv.Shutdown()
		t.Fatalf("new client: %v", err)
	}

	subject := SubjectForEvent("gw-1", eventType)
	messages := make(chan *nats.Msg, 1)
	sub, err := client.Subscribe(subject, func(msg *nats.Msg) {
		messages <- msg
	})
	if err != nil {
		_ = client.Close()
		srv.Shutdown()
		t.Fatalf("subscribe: %v", err)
	}

	publisher := NewPublisher(client, "gw-1", nil)
	return publisher, subject, messages, func() {
		_ = sub.Unsubscribe()
		_ = client.Close()
		srv.Shutdown()
	}
}

func expectEventEnvelope(t *testing.T, messages <-chan *nats.Msg, expectedSubject string) EventEnvelope {
	t.Helper()

	select {
	case msg := <-messages:
		if msg.Subject != expectedSubject {
			t.Fatalf("expected subject %q, got %q", expectedSubject, msg.Subject)
		}

		var envelope EventEnvelope
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if envelope.Timestamp.IsZero() {
			t.Fatal("expected non-zero envelope timestamp")
		}
		return envelope
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event message")
	}

	return EventEnvelope{}
}

