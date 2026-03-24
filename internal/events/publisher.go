package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const maxTCPPort = 65535

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
	occurredAt := time.Now().UTC()
	host := normalizeEndpointHost(server.Host)
	endpoint := host
	if host != "" && isValidTCPPort(server.Port) {
		endpoint = net.JoinHostPort(host, strconv.Itoa(server.Port))
	}
	endpointURL := canonicalEndpointURL(host, server.Protocol, server.Port)
	allowedEndpoints := []string{}
	if endpointURL != "" {
		allowedEndpoints = append(allowedEndpoints, endpointURL)
	}
	payload := ServerRegisteredPayload{
		EventID:          newEventID(),
		OccurredAt:       occurredAt,
		ServerID:         server.ID,
		RegistrationID:   server.ID,
		LeaseEpoch:       server.LeaseEpoch,
		CapabilityHash:   strings.TrimSpace(server.CapabilityHash),
		SyncState:        strings.TrimSpace(server.SyncState.String()),
		Name:             server.Name,
		SPIFFEID:         server.SPIFFEID,
		Capabilities:     server.Capabilities,
		Endpoint:         endpoint,
		EndpointURL:      endpointURL,
		AllowedEndpoints: allowedEndpoints,
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

func newEventID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("evt-%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("evt-%s", hex.EncodeToString(bytes))
}

func canonicalEndpointURL(host, protocol string, port int) string {
	host = normalizeEndpointHost(host)
	if host == "" {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(protocol))
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	hostPort := host
	if isValidTCPPort(port) {
		hostPort = net.JoinHostPort(host, strconv.Itoa(port))
	} else if isIPv6Literal(host) {
		hostPort = "[" + host + "]"
	}
	return (&url.URL{
		Scheme: scheme,
		Host:   hostPort,
	}).String()
}

func normalizeEndpointHost(host string) string {
	host = strings.TrimSpace(host)
	if len(host) > 2 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSpace(host[1 : len(host)-1])
	}
	return host
}

func isValidTCPPort(port int) bool {
	return port > 0 && port <= maxTCPPort
}

func isIPv6Literal(host string) bool {
	return strings.Contains(host, ":") && net.ParseIP(host) != nil
}
