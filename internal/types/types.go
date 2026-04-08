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

// RegistrationSyncState tracks whether platform-side ingestion has acknowledged
// a server registration lease.
type RegistrationSyncState string

const (
	// SyncStateUnacked means the gateway has not yet received platform ack for
	// the current lease epoch.
	SyncStateUnacked RegistrationSyncState = "unacked"
	// SyncStateAcked means the current lease epoch has been acknowledged by the
	// platform registry pipeline.
	SyncStateAcked RegistrationSyncState = "acked"
	// SyncStateStale means ack information is outdated for the current lease.
	SyncStateStale RegistrationSyncState = "stale"
)

// String returns the string value of the registration sync state.
func (s RegistrationSyncState) String() string {
	return string(s)
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

// ContentBlock is a normalized tool result content entry.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolResult is a normalized result returned from a tool call.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"is_error"`
}

// ServerRecord is the persisted representation of a registered MCP server.
type ServerRecord struct {
	ID                     string                `json:"id"`
	Name                   string                `json:"name"`
	SPIFFEID               string                `json:"spiffe_id"`
	Version                string                `json:"version"`
	Host                   string                `json:"host"`
	Port                   int                   `json:"port"`
	Protocol               string                `json:"protocol"`
	Capabilities           Capability            `json:"capabilities"`
	Status                 ServerStatus          `json:"status"`
	PolicyProfile          string                `json:"policy_profile"`
	LastHeartbeat          *time.Time            `json:"last_heartbeat"`
	LeaseEpoch             int64                 `json:"lease_epoch"`
	CapabilityHash         string                `json:"capability_hash"`
	SyncState              RegistrationSyncState `json:"sync_state"`
	LastPlatformAckVersion string                `json:"last_platform_ack_version"`
	LastPlatformAckAt      *time.Time            `json:"last_platform_ack_at"`
	RateLimit              *int                  `json:"rate_limit,omitempty"`
	RateBurst              *int                  `json:"rate_burst,omitempty"`
	RoutingStrategy        string                `json:"routing_strategy,omitempty"`
	CreatedAt              time.Time             `json:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
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

// APIKey is a persisted API key lookup and verification record.
type APIKey struct {
	ID            string     `json:"id"`
	KeyLookupHash string     `json:"key_lookup_hash"`
	KeyBcryptHash string     `json:"key_bcrypt_hash"`
	Name          string     `json:"name"`
	Scopes        []string   `json:"scopes"`
	ClientID      string     `json:"client_id"`
	PolicyProfile string     `json:"policy_profile"`
	ServerScope   []string   `json:"server_scope,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// AuditEntry is a persisted security and policy audit event.
type AuditEntry struct {
	ID         string         `json:"id"`
	EventType  string         `json:"event_type"`
	ClientID   string         `json:"client_id"`
	Username   string         `json:"username"`
	ServerName string         `json:"server_name"`
	Details    map[string]any `json:"details"`
	CreatedAt  time.Time      `json:"created_at"`
}

// ServerFilter defines optional filtering for server listing queries.
type ServerFilter struct {
	Status      *ServerStatus `json:"status"`
	NamePattern string        `json:"name_pattern"`
	Limit       int           `json:"limit"`
	Offset      int           `json:"offset"`
}

// RiskLevel classifies the risk associated with a tool.
type RiskLevel string

const (
	// RiskReadOnly indicates a tool that only reads data.
	RiskReadOnly RiskLevel = "read_only"
	// RiskWrite indicates a tool that creates or modifies data.
	RiskWrite RiskLevel = "write"
	// RiskDestructive indicates a tool that deletes or destroys data.
	RiskDestructive RiskLevel = "destructive"
	// RiskUnknown indicates a tool whose risk has not been classified.
	RiskUnknown RiskLevel = "unknown"
)

// String returns the string value of the risk level.
func (r RiskLevel) String() string {
	return string(r)
}

// IsValid reports whether the risk level is a recognized value.
func (r RiskLevel) IsValid() bool {
	switch r {
	case RiskReadOnly, RiskWrite, RiskDestructive, RiskUnknown:
		return true
	}
	return false
}

// ToolClassification is the persisted risk classification of a tool.
type ToolClassification struct {
	ToolName       string    `json:"tool_name"`
	RiskLevel      RiskLevel `json:"risk_level"`
	Reason         string    `json:"reason"`
	AutoClassified bool      `json:"auto_classified"`
	UpdatedBy      string    `json:"updated_by"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ToolFilter defines optional filtering for tool classification queries.
type ToolFilter struct {
	Query     string    `json:"query"`
	RiskLevel RiskLevel `json:"risk_level"`
	Limit     int       `json:"limit"`
	Offset    int       `json:"offset"`
}

// UserRole represents the access level of a gateway user.
type UserRole string

const (
	// RoleAdmin grants unrestricted tool access and dashboard management.
	RoleAdmin UserRole = "admin"
	// RoleUser grants tool access limited to explicitly allowed tools.
	RoleUser UserRole = "user"
	// RoleViewer grants read-only visibility of allowed tools with no call access.
	RoleViewer UserRole = "viewer"
	// RoleBlocked denies all tool access and dashboard visibility.
	RoleBlocked UserRole = "blocked"
)

// String returns the string value of the user role.
func (r UserRole) String() string {
	return string(r)
}

// IsValid reports whether the user role is a recognized value.
func (r UserRole) IsValid() bool {
	switch r {
	case RoleAdmin, RoleUser, RoleViewer, RoleBlocked:
		return true
	}
	return false
}

// User is a persisted gateway user record linked to an OIDC identity.
type User struct {
	ID          string    `json:"id"`
	OIDCSub     string    `json:"oidc_sub"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        UserRole  `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserToolPermission is a persisted tool access grant for a user.
type UserToolPermission struct {
	UserID    string    `json:"user_id"`
	ToolName  string    `json:"tool_name"`
	GrantedBy string    `json:"granted_by"`
	GrantedAt time.Time `json:"granted_at"`
}

// UserFilter defines optional filtering for user listing queries.
type UserFilter struct {
	Query  string   `json:"query"`
	Role   UserRole `json:"role"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
}

// AuditFilter defines optional filtering for audit log queries.
type AuditFilter struct {
	EventType     string     `json:"event_type"`
	ClientID      string     `json:"client_id"`
	Username      string     `json:"username"`
	ServerName    string     `json:"server_name"`
	DetailsSearch string     `json:"details_search"`
	SortBy        string     `json:"sort_by"`
	SortDir       string     `json:"sort_dir"`
	Since         *time.Time `json:"since"`
	Until         *time.Time `json:"until"`
	Limit         int        `json:"limit"`
	Offset        int        `json:"offset"`
}
