package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

const (
	// EventServerRegistered is emitted when a backend registration is accepted.
	EventServerRegistered = "server.registered"
	// EventServerDeregistered is emitted when a backend is deregistered.
	EventServerDeregistered = "server.deregistered"
	// EventServerHealthChanged is emitted when backend health status changes.
	EventServerHealthChanged = "server.health_changed"
	// EventPolicyViolated is emitted when a policy violation occurs.
	EventPolicyViolated = "policy.violated"

	// ConfigScopePolicy is the policy configuration subject suffix.
	ConfigScopePolicy = "policy"
	// ConfigScopeServers is the server configuration subject suffix.
	ConfigScopeServers = "servers"
	// ConfigScopeServerSettings is the server settings configuration subject suffix.
	ConfigScopeServerSettings = "server_settings"
	// ConfigScopeAuth is the auth configuration subject suffix.
	ConfigScopeAuth = "auth"
)

// EventEnvelope wraps all outbound/inbound NATS events.
type EventEnvelope struct {
	EventType string          `json:"event_type"`
	Timestamp time.Time       `json:"timestamp"`
	GatewayID string          `json:"gateway_id"`
	Payload   json.RawMessage `json:"payload"`
}

// ServerRegisteredPayload is the payload for EventServerRegistered.
type ServerRegisteredPayload struct {
	ServerID     string           `json:"server_id"`
	Name         string           `json:"name"`
	SPIFFEID     string           `json:"spiffe_id"`
	Capabilities types.Capability `json:"capabilities"`
	Endpoint     string           `json:"endpoint"`
}

// ServerDeregisteredPayload is the payload for EventServerDeregistered.
type ServerDeregisteredPayload struct {
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
}

// ServerHealthChangedPayload is the payload for EventServerHealthChanged.
type ServerHealthChangedPayload struct {
	ServerID  string             `json:"server_id"`
	Name      string             `json:"name"`
	OldStatus types.ServerStatus `json:"old_status"`
	NewStatus types.ServerStatus `json:"new_status"`
}

// PolicyViolatedPayload is the payload for EventPolicyViolated.
type PolicyViolatedPayload struct {
	ClientID   string   `json:"client_id"`
	ToolName   string   `json:"tool_name"`
	Violations []string `json:"violations"`
	Decision   string   `json:"decision"`
}

// SubjectForEvent returns the publish subject for a gateway-scoped event type.
func SubjectForEvent(gatewayID string, eventType string) string {
	gateway := strings.TrimSpace(gatewayID)
	if gateway == "" {
		gateway = "unknown"
	}
	evt := strings.TrimSpace(eventType)
	return fmt.Sprintf("mcpgw.%s.events.%s", gateway, evt)
}

// SubjectForConfig returns the subscribe subject for a gateway-scoped config scope.
func SubjectForConfig(gatewayID string, scope string) string {
	gateway := strings.TrimSpace(gatewayID)
	if gateway == "" {
		gateway = "unknown"
	}
	cfgScope := strings.TrimSpace(scope)
	return fmt.Sprintf("mcpgw.%s.config.%s", gateway, cfgScope)
}
