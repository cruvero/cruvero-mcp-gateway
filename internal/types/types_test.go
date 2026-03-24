package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestServerStatusHelpers(t *testing.T) {
	t.Parallel()

	if StatusActive.String() != "active" {
		t.Fatalf("expected active string, got %q", StatusActive.String())
	}
	if !StatusExpired.IsTerminal() {
		t.Fatalf("expected expired status to be terminal")
	}
	if StatusPending.IsTerminal() {
		t.Fatalf("expected pending status to be non-terminal")
	}
	if !StatusActive.IsRoutable() {
		t.Fatalf("expected active status to be routable")
	}
	if StatusStale.IsRoutable() {
		t.Fatalf("expected stale status to be non-routable")
	}
}

func TestEnforcementModeString(t *testing.T) {
	t.Parallel()

	if ModeEnforce.String() != "enforce" {
		t.Fatalf("expected enforce string, got %q", ModeEnforce.String())
	}
	if ModeAudit.String() != "audit" {
		t.Fatalf("expected audit string, got %q", ModeAudit.String())
	}
}

func TestServerRecordJSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	record := ServerRecord{
		ID:       "server-1",
		Name:     "alpha",
		SPIFFEID: "spiffe://trust/ns/default/sa/alpha",
		Version:  "1.0.0",
		Host:     "alpha.svc",
		Port:     8080,
		Capabilities: Capability{
			Tools:     []string{"tool.a"},
			Resources: []string{"res://alpha"},
			Prompts:   []string{"prompt.a"},
		},
		Status:        StatusActive,
		PolicyProfile: "default",
		LastHeartbeat: &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal server record: %v", err)
	}

	var got ServerRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal server record: %v", err)
	}

	if got.SPIFFEID != record.SPIFFEID {
		t.Fatalf("expected spiffe_id %q, got %q", record.SPIFFEID, got.SPIFFEID)
	}
	if got.Status != StatusActive {
		t.Fatalf("expected status active, got %q", got.Status)
	}
}

func TestPolicyProfileAndHealthStatusJSON(t *testing.T) {
	t.Parallel()

	profile := PolicyProfile{
		Name:            "default",
		RateLimit:       10,
		RateBurst:       20,
		ToolAllowlist:   []string{"safe.tool"},
		ToolDenylist:    []string{"dangerous.tool"},
		EnforcementMode: ModeEnforce,
	}

	_, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal policy profile: %v", err)
	}

	health := HealthStatus{
		Status:            "ok",
		Version:           "dev",
		Uptime:            "10s",
		RegisteredServers: 2,
		NATSConnected:     true,
	}

	_, err = json.Marshal(health)
	if err != nil {
		t.Fatalf("marshal health status: %v", err)
	}
}

func TestAPIKeyAuditAndFilterJSON(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	status := StatusActive

	apiKey := APIKey{
		ID:            "key-1",
		KeyLookupHash: "lookup-hash",
		KeyBcryptHash: "bcrypt-hash",
		Name:          "integration-key",
		Scopes:        []string{"read", "write"},
		ClientID:      "client-1",
		ExpiresAt:     &now,
		CreatedAt:     now,
	}
	if _, err := json.Marshal(apiKey); err != nil {
		t.Fatalf("marshal api key: %v", err)
	}

	auditEntry := AuditEntry{
		ID:         "audit-1",
		EventType:  "auth.success",
		ClientID:   "client-1",
		Username:   "client-1",
		ServerName: "alpha",
		Details: map[string]any{
			"tool": "safe.tool",
		},
		CreatedAt: now,
	}
	if _, err := json.Marshal(auditEntry); err != nil {
		t.Fatalf("marshal audit entry: %v", err)
	}

	serverFilter := ServerFilter{
		Status:      &status,
		NamePattern: "alp%",
		Limit:       10,
		Offset:      5,
	}
	if _, err := json.Marshal(serverFilter); err != nil {
		t.Fatalf("marshal server filter: %v", err)
	}

	auditFilter := AuditFilter{
		EventType:  "policy.deny",
		ClientID:   "client-2",
		Username:   "client-2",
		ServerName: "beta",
		SortBy:     "created_at",
		SortDir:    "desc",
		Since:      &now,
		Until:      &now,
		Limit:      50,
		Offset:     10,
	}
	if _, err := json.Marshal(auditFilter); err != nil {
		t.Fatalf("marshal audit filter: %v", err)
	}
}

func TestRegistrationSyncStateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state RegistrationSyncState
		want  string
	}{
		{name: "unacked", state: SyncStateUnacked, want: "unacked"},
		{name: "acked", state: SyncStateAcked, want: "acked"},
		{name: "stale", state: SyncStateStale, want: "stale"},
		{name: "custom value", state: RegistrationSyncState("custom"), want: "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.state.String(); got != tt.want {
				t.Fatalf("RegistrationSyncState.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRiskLevelString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level RiskLevel
		want  string
	}{
		{name: "read_only", level: RiskReadOnly, want: "read_only"},
		{name: "write", level: RiskWrite, want: "write"},
		{name: "destructive", level: RiskDestructive, want: "destructive"},
		{name: "unknown", level: RiskUnknown, want: "unknown"},
		{name: "custom value", level: RiskLevel("custom"), want: "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.level.String(); got != tt.want {
				t.Fatalf("RiskLevel.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRiskLevelIsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level RiskLevel
		want  bool
	}{
		{name: "read_only is valid", level: RiskReadOnly, want: true},
		{name: "write is valid", level: RiskWrite, want: true},
		{name: "destructive is valid", level: RiskDestructive, want: true},
		{name: "unknown is valid", level: RiskUnknown, want: true},
		{name: "empty string is invalid", level: RiskLevel(""), want: false},
		{name: "arbitrary string is invalid", level: RiskLevel("not_a_level"), want: false},
		{name: "uppercase variant is invalid", level: RiskLevel("READ_ONLY"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.level.IsValid(); got != tt.want {
				t.Fatalf("RiskLevel(%q).IsValid() = %v, want %v", tt.level, got, tt.want)
			}
		})
	}
}

func TestUserRoleString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role UserRole
		want string
	}{
		{name: "admin", role: RoleAdmin, want: "admin"},
		{name: "user", role: RoleUser, want: "user"},
		{name: "viewer", role: RoleViewer, want: "viewer"},
		{name: "blocked", role: RoleBlocked, want: "blocked"},
		{name: "custom value", role: UserRole("custom"), want: "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.role.String(); got != tt.want {
				t.Fatalf("UserRole.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserRoleIsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role UserRole
		want bool
	}{
		{name: "admin is valid", role: RoleAdmin, want: true},
		{name: "user is valid", role: RoleUser, want: true},
		{name: "viewer is valid", role: RoleViewer, want: true},
		{name: "blocked is valid", role: RoleBlocked, want: true},
		{name: "empty string is invalid", role: UserRole(""), want: false},
		{name: "arbitrary string is invalid", role: UserRole("not_a_role"), want: false},
		{name: "uppercase variant is invalid", role: UserRole("ADMIN"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.role.IsValid(); got != tt.want {
				t.Fatalf("UserRole(%q).IsValid() = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestNormalizeAuditSortBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "created_at", input: "created_at", expect: AuditSortByCreatedAt},
		{name: "event_type", input: "event_type", expect: AuditSortByEventType},
		{name: "username", input: "username", expect: AuditSortByUsername},
		{name: "server_name", input: "server_name", expect: AuditSortByServerName},
		{name: "trim and lowercase", input: "  USERNAME  ", expect: AuditSortByUsername},
		{name: "fallback", input: "drop table", expect: AuditSortByCreatedAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeAuditSortBy(tt.input); got != tt.expect {
				t.Fatalf("NormalizeAuditSortBy(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestNormalizeAuditSortDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "asc", input: "asc", expect: AuditSortDirAsc},
		{name: "uppercase asc", input: "ASC", expect: AuditSortDirAsc},
		{name: "desc", input: "desc", expect: AuditSortDirDesc},
		{name: "fallback", input: "invalid", expect: AuditSortDirDesc},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeAuditSortDir(tt.input); got != tt.expect {
				t.Fatalf("NormalizeAuditSortDir(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
