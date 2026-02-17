package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

// Publisher publishes gateway lifecycle and policy events.
type Publisher struct {
	client    *Client
	gatewayID string
	logger    *slog.Logger
}

// NewPublisher creates a new events publisher.
func NewPublisher(client *Client, gatewayID string, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	gateway := strings.TrimSpace(gatewayID)
	if gateway == "" && client != nil {
		gateway = client.GatewayID()
	}

	return &Publisher{
		client:    client,
		gatewayID: gateway,
		logger:    logger,
	}
}

func (p *Publisher) publish(ctx context.Context, eventType string, payload any) error {
	if p == nil || p.client == nil || !p.client.IsConnected() {
		// Gracefully no-op when disconnected/unconfigured.
		return nil
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publish %s event: marshal payload: %w", eventType, err)
	}

	envelopeBytes, err := json.Marshal(EventEnvelope{
		EventType: strings.TrimSpace(eventType),
		Timestamp: time.Now().UTC(),
		GatewayID: p.gatewayID,
		Payload:   payloadBytes,
	})
	if err != nil {
		return fmt.Errorf("publish %s event: marshal envelope: %w", eventType, err)
	}

	subject := SubjectForEvent(p.gatewayID, eventType)
	if err := p.client.Publish(subject, envelopeBytes); err != nil {
		return fmt.Errorf("publish %s event: %w", eventType, err)
	}

	p.logger.DebugContext(ctx, "published event", slog.String("event_type", eventType), slog.String("subject", subject))
	return nil
}

// PublishServerRegistered publishes a server.registered event.
func (p *Publisher) PublishServerRegistered(ctx context.Context, server types.ServerRecord) error {
	payload := ServerRegisteredPayload{
		ServerID:     server.ID,
		Name:         server.Name,
		SPIFFEID:     server.SPIFFEID,
		Capabilities: server.Capabilities,
		Endpoint:     fmt.Sprintf("%s:%d", strings.TrimSpace(server.Host), server.Port),
	}
	return p.publish(ctx, EventServerRegistered, payload)
}

// PublishServerDeregistered publishes a server.deregistered event.
func (p *Publisher) PublishServerDeregistered(ctx context.Context, serverID string, name string, reason string) error {
	payload := ServerDeregisteredPayload{
		ServerID: strings.TrimSpace(serverID),
		Name:     strings.TrimSpace(name),
		Reason:   strings.TrimSpace(reason),
	}
	return p.publish(ctx, EventServerDeregistered, payload)
}

// PublishServerHealthChanged publishes a server.health_changed event.
func (p *Publisher) PublishServerHealthChanged(
	ctx context.Context,
	serverID string,
	name string,
	oldStatus types.ServerStatus,
	newStatus types.ServerStatus,
) error {
	payload := ServerHealthChangedPayload{
		ServerID:  strings.TrimSpace(serverID),
		Name:      strings.TrimSpace(name),
		OldStatus: oldStatus,
		NewStatus: newStatus,
	}
	return p.publish(ctx, EventServerHealthChanged, payload)
}

// PublishPolicyViolated publishes a policy.violated event.
func (p *Publisher) PublishPolicyViolated(
	ctx context.Context,
	clientID string,
	toolName string,
	violations []string,
	decision string,
) error {
	payload := PolicyViolatedPayload{
		ClientID:   strings.TrimSpace(clientID),
		ToolName:   strings.TrimSpace(toolName),
		Violations: append([]string(nil), violations...),
		Decision:   strings.TrimSpace(decision),
	}
	return p.publish(ctx, EventPolicyViolated, payload)
}

