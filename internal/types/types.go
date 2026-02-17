package types

import "time"

// ServerStatus represents the lifecycle status of a registered MCP server.
type ServerStatus string

const (
	// StatusPending indicates registration was received but not yet approved.
	StatusPending ServerStatus = "pending"
	// StatusApproved indicates registration is approved and waiting for heartbeat.
	StatusApproved ServerStatus = "approved"
	// StatusActive indicates server is healthy and routable.
	StatusActive ServerStatus = "active"
	// StatusStale indicates server heartbeat is overdue.
	StatusStale ServerStatus = "stale"
	// StatusExpired indicates server registration is no longer valid.
	StatusExpired ServerStatus = "expired"
)

// String returns the string value of the server status.
func (s ServerStatus) String() string {
	return string(s)
}

// IsTerminal returns true when the server status is permanently terminal.
func (s ServerStatus) IsTerminal() bool {
	return s == StatusExpired
}

// IsRoutable returns true only when the server is active.
func (s ServerStatus) IsRoutable() bool {
	return s == StatusActive
}

// EnforcementMode controls policy handling behavior.
type EnforcementMode string

const (
	// ModeEnforce blocks requests that violate policy.
	ModeEnforce EnforcementMode = "enforce"
	// ModeAudit records policy violations without blocking requests.
	ModeAudit EnforcementMode = "audit"
)

// String returns the string value of the enforcement mode.
func (m EnforcementMode) String() string {
	return string(m)
}

// Capability describes tools, resources, and prompts exposed by an MCP server.
type Capability struct {
	Tools     []string `json:"tools"`
	Resources []string `json:"resources"`
	Prompts   []string `json:"prompts"`
}

// ServerRecord is the persisted representation of a registered MCP server.
type ServerRecord struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	SPIFFEID      string       `json:"spiffe_id"`
	Version       string       `json:"version"`
	Host          string       `json:"host"`
	Port          int          `json:"port"`
	Capabilities  Capability   `json:"capabilities"`
	Status        ServerStatus `json:"status"`
	PolicyProfile string       `json:"policy_profile"`
	LastHeartbeat *time.Time   `json:"last_heartbeat"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// Registration is the request payload for backend server registration.
type Registration struct {
	ServiceName   string            `json:"service_name"`
	Version       string            `json:"version"`
	ListenAddress string            `json:"listen_address"`
	Capabilities  Capability        `json:"capabilities"`
	Labels        map[string]string `json:"labels"`
}

// PolicyProfile defines rate limits and tool policy behavior for a client/profile.
type PolicyProfile struct {
	Name            string          `json:"name"`
	RateLimit       int             `json:"rate_limit"`
	RateBurst       int             `json:"rate_burst"`
	ToolAllowlist   []string        `json:"tool_allowlist"`
	ToolDenylist    []string        `json:"tool_denylist"`
	EnforcementMode EnforcementMode `json:"enforcement_mode"`
}

// HealthStatus is the API response shape for health and readiness endpoints.
type HealthStatus struct {
	Status            string `json:"status"`
	Version           string `json:"version"`
	Uptime            string `json:"uptime"`
	RegisteredServers int    `json:"registered_servers"`
	NATSConnected     bool   `json:"nats_connected"`
}
