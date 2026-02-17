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
