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

	// AckScopeServerRegistered is used for platform ack messages that confirm
	// tool-registry ingestion of a server registration lease.
	AckScopeServerRegistered = "server_registered"

	// ConfigScopePolicy is the policy configuration subject suffix.
	ConfigScopePolicy = "policy"
	// ConfigScopeServers is the server configuration subject suffix.
	ConfigScopeServers = "servers"
	// ConfigScopeServerSettings is the server settings configuration subject suffix.
	ConfigScopeServerSettings = "server_settings"
	// ConfigScopeAuth is the auth configuration subject suffix.
	ConfigScopeAuth = "auth"
	// ConfigScopeToolMetadata is the tool metadata configuration subject suffix.
	ConfigScopeToolMetadata = "tool_metadata"
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
	EventID         string                `json:"event_id,omitempty"`
	OccurredAt      time.Time             `json:"occurred_at,omitempty"`
	ServerID        string                `json:"server_id"`
	RegistrationID  string                `json:"registration_id,omitempty"`
	LeaseEpoch      int64                 `json:"lease_epoch,omitempty"`
	CapabilityHash  string                `json:"capability_hash,omitempty"`
	SyncState       string                `json:"sync_state,omitempty"`
	Name            string                `json:"name"`
	SPIFFEID        string                `json:"spiffe_id"`
	Capabilities    types.Capability      `json:"capabilities"`
	Endpoint        string                `json:"endpoint"`
	ToolDefinitions []ToolDefinitionPayload `json:"tool_definitions,omitempty"`
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

// ServerRegisteredAckPayload is emitted by the platform after processing a
// registration event and refreshing tool registry state.
type ServerRegisteredAckPayload struct {
	RegistrationID  string    `json:"registration_id"`
	LeaseEpoch      int64     `json:"lease_epoch"`
	CapabilityHash  string    `json:"capability_hash,omitempty"`
	RegistryVersion string    `json:"registry_version,omitempty"`
	ToolSchemaHash  string    `json:"tool_schema_hash,omitempty"`
	AckedAt         time.Time `json:"acked_at,omitempty"`
}

// ToolMetadataConfigMessage describes incoming tool metadata enrichment updates.
type ToolMetadataConfigMessage struct {
	Version int64               `json:"version"`
	Tools   []ToolMetadataEntry `json:"tools"`
}

// ToolMetadataEntry describes metadata for a single tool.
type ToolMetadataEntry struct {
	ToolName    string         `json:"tool_name"`
	Category    string         `json:"category"`
	DisplayName string         `json:"display_name,omitempty"`
	Summary     string         `json:"summary,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Priority    int            `json:"priority,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ToolDefinitionPayload is a serializable tool definition for event payloads.
type ToolDefinitionPayload struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Annotations  json.RawMessage `json:"annotations,omitempty"`
	DeferLoading bool            `json:"defer_loading,omitempty"`
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

// SubjectForConfigRequest returns the subject used to request a full config snapshot.
func SubjectForConfigRequest(gatewayID string) string {
	gateway := strings.TrimSpace(gatewayID)
	if gateway == "" {
		gateway = "unknown"
	}
	return fmt.Sprintf("mcpgw.%s.config.request", gateway)
}

// SubjectForAck returns the publish/subscribe subject for gateway-scoped
// acknowledgement messages.
func SubjectForAck(gatewayID string, scope string) string {
	gateway := strings.TrimSpace(gatewayID)
	if gateway == "" {
		gateway = "unknown"
	}
	ackScope := strings.TrimSpace(scope)
	return fmt.Sprintf("mcpgw.%s.acks.%s", gateway, ackScope)
}
