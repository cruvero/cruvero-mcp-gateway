package registration

import (
	"fmt"
	"net"
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
	host := strings.TrimSpace(r.Listen.Host)
	if host == "" {
		return fmt.Errorf("listen.host is required")
	}
	if err := validateHost(host); err != nil {
		return fmt.Errorf("listen.host: %w", err)
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

	if err := validateCapabilityNames("tools", r.Capabilities.Tools); err != nil {
		return err
	}
	if err := validateCapabilityNames("resources", r.Capabilities.Resources); err != nil {
		return err
	}
	return validateCapabilityNames("prompts", r.Capabilities.Prompts)
}

// validateCapabilityNames checks that no capability name in the list is blank.
func validateCapabilityNames(kind string, names []string) error {
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("capabilities.%s cannot contain empty names", kind)
		}
	}
	return nil
}

// validateHost blocks SSRF-prone hosts: loopback, link-local, unspecified,
// localhost, and cloud metadata endpoints. Private RFC 1918 addresses are
// intentionally allowed because MCP servers in Kubernetes use pod IPs.
func validateHost(host string) error {
	// Normalize: strip bracketed IPv6 (e.g. "[::1]") and trailing DNS dot.
	if len(host) > 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	host = strings.TrimRight(host, ".")

	lower := strings.ToLower(host)

	metadataHosts := []string{
		"169.254.169.254",
		"metadata.google.internal",
		"metadata.goog",
	}
	for _, m := range metadataHosts {
		if lower == m {
			return fmt.Errorf("cloud metadata endpoint not allowed")
		}
	}

	if ip := net.ParseIP(host); ip != nil {
		if err := validateIP(ip); err != nil {
			return err
		}
	}

	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("localhost not allowed")
	}

	return nil
}

// validateIP checks a parsed IP for SSRF-prone addresses.
func validateIP(ip net.IP) error {
	// Normalize IPv4-mapped IPv6 (e.g. ::ffff:127.0.0.1) to IPv4.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("loopback and link-local addresses not allowed")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address not allowed")
	}
	// Re-check metadata IP after normalization.
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("cloud metadata endpoint not allowed")
	}
	return nil
}
