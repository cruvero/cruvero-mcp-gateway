package registration

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cruvero/mcp-gateway/internal/types"
)

var serviceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// RegistrationRequest is the payload sent by MCP servers during registration.
type RegistrationRequest struct {
	ServiceName  string            `json:"service_name"`
	Version      string            `json:"version"`
	Listen       ListenConfig      `json:"listen"`
	Capabilities types.Capability  `json:"capabilities"`
	Labels       map[string]string `json:"labels"`
}

// ListenConfig describes how the MCP server can be reached.
type ListenConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// RegistrationResponse is returned after a successful registration handshake.
type RegistrationResponse struct {
	InstanceID               string                      `json:"instance_id"`
	RegistrationID           string                      `json:"registration_id"`
	LeaseEpoch               int64                       `json:"lease_epoch"`
	CapabilityHash           string                      `json:"capability_hash"`
	SyncState                types.RegistrationSyncState `json:"sync_state"`
	LastPlatformAckVersion   string                      `json:"last_platform_ack_version,omitempty"`
	PolicySnapshot           *types.PolicyProfile        `json:"policy_snapshot"`
	HeartbeatInterval        int                         `json:"heartbeat_interval"`
	HeartbeatIntervalSeconds int                         `json:"heartbeat_interval_seconds"`
	ConfigVersion            int64                       `json:"config_version"`
	EffectiveSettings        map[string]any              `json:"effective_settings"`
	Status                   types.ServerStatus          `json:"status"`
}

// Validate validates untrusted registration payload input.
func (r RegistrationRequest) Validate() error {
	if !serviceNamePattern.MatchString(strings.TrimSpace(r.ServiceName)) {
		return fmt.Errorf("invalid service_name format")
	}
	if strings.TrimSpace(r.Listen.Host) == "" {
		return fmt.Errorf("listen.host is required")
	}
	if r.Listen.Port < 1 || r.Listen.Port > 65535 {
		return fmt.Errorf("listen.port must be between 1 and 65535")
	}

	protocol := strings.TrimSpace(strings.ToLower(r.Listen.Protocol))
	if protocol != "" && protocol != "http" && protocol != "https" {
		return fmt.Errorf("listen.protocol must be http or https when provided")
	}

	if len(r.Capabilities.Tools) == 0 && len(r.Capabilities.Resources) == 0 && len(r.Capabilities.Prompts) == 0 {
		return fmt.Errorf("at least one capability must be provided")
	}

	for _, tool := range r.Capabilities.Tools {
		if strings.TrimSpace(tool) == "" {
			return fmt.Errorf("capabilities.tools cannot contain empty names")
		}
	}
	for _, resource := range r.Capabilities.Resources {
		if strings.TrimSpace(resource) == "" {
			return fmt.Errorf("capabilities.resources cannot contain empty names")
		}
	}
	for _, prompt := range r.Capabilities.Prompts {
		if strings.TrimSpace(prompt) == "" {
			return fmt.Errorf("capabilities.prompts cannot contain empty names")
		}
	}

	return nil
}
