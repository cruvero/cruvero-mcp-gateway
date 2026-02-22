package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestSubjectForEvent(t *testing.T) {
	t.Parallel()

	got := SubjectForEvent("gw-1", EventServerRegistered)
	if got != "mcpgw.gw-1.events.server.registered" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestSubjectForConfig(t *testing.T) {
	t.Parallel()

	got := SubjectForConfig("gw-1", ConfigScopePolicy)
	if got != "mcpgw.gw-1.config.policy" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestSubjectForConfigRequest(t *testing.T) {
	t.Parallel()

	got := SubjectForConfigRequest("gw-1")
	if got != "mcpgw.gw-1.config.request" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestSubjectForAck(t *testing.T) {
	t.Parallel()

	got := SubjectForAck("gw-1", AckScopeServerRegistered)
	if got != "mcpgw.gw-1.acks.server_registered" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestEventEnvelopeJSONRoundTrip(t *testing.T) {
	t.Parallel()

	payload := ServerDeregisteredPayload{
		ServerID: "server-1",
		Name:     "alpha",
		Reason:   "expired",
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	input := EventEnvelope{
		EventType: EventServerDeregistered,
		Timestamp: time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC),
		GatewayID: "gw-1",
		Payload:   rawPayload,
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var decoded EventEnvelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if decoded.EventType != EventServerDeregistered {
		t.Fatalf("expected %q, got %q", EventServerDeregistered, decoded.EventType)
	}
	if decoded.GatewayID != "gw-1" {
		t.Fatalf("expected gateway id gw-1, got %q", decoded.GatewayID)
	}
}

func TestServerRegisteredPayload_WithToolDefinitions(t *testing.T) {
	t.Parallel()

	payload := ServerRegisteredPayload{
		ServerID: "server-1",
		Name:     "alpha",
		SPIFFEID: "spiffe://cluster/ns/default/sa/alpha",
		Capabilities: types.Capability{
			Tools: []string{"tool.echo"},
		},
		Endpoint: "alpha.default.svc:8443",
		ToolDefinitions: []ToolDefinitionPayload{
			{Name: "echo", Description: "Echo tool", DeferLoading: true},
			{Name: "ping", Description: "Ping tool", Annotations: json.RawMessage(`{"readOnlyHint":true}`)},
		},
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify tool_definitions key is present.
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["tool_definitions"]; !ok {
		t.Fatal("expected tool_definitions key in JSON")
	}

	// Round-trip.
	var decoded ServerRegisteredPayload
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.ToolDefinitions) != 2 {
		t.Fatalf("expected 2 tool definitions, got %d", len(decoded.ToolDefinitions))
	}
	if decoded.ToolDefinitions[0].Name != "echo" {
		t.Fatalf("expected first tool name 'echo', got %q", decoded.ToolDefinitions[0].Name)
	}
	if !decoded.ToolDefinitions[0].DeferLoading {
		t.Fatal("expected DeferLoading=true")
	}
}

func TestServerRegisteredPayload_WithoutToolDefinitions(t *testing.T) {
	t.Parallel()

	payload := ServerRegisteredPayload{
		ServerID: "server-1",
		Name:     "alpha",
		SPIFFEID: "spiffe://cluster/ns/default/sa/alpha",
		Endpoint: "alpha.default.svc:8443",
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify tool_definitions key is absent (omitempty).
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := raw["tool_definitions"]; ok {
		t.Fatal("expected tool_definitions key to be absent when nil")
	}
}

func TestToolMetadataConfigMessage_RoundTrip(t *testing.T) {
	t.Parallel()

	msg := ToolMetadataConfigMessage{
		Version: 42,
		Tools: []ToolMetadataEntry{
			{
				ToolName:    "mcp.github.create_issue",
				Category:    "github",
				DisplayName: "Create Issue",
				Summary:     "Creates a new issue",
				Tags:        []string{"vcs", "issues"},
				Priority:    10,
				Metadata:    map[string]any{"custom_key": "value"},
			},
			{
				ToolName: "mcp.slack.send_message",
				Category: "slack",
			},
		},
	}

	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ToolMetadataConfigMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Version != 42 {
		t.Fatalf("expected version 42, got %d", decoded.Version)
	}
	if len(decoded.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(decoded.Tools))
	}
	if decoded.Tools[0].DisplayName != "Create Issue" {
		t.Fatalf("expected display_name 'Create Issue', got %q", decoded.Tools[0].DisplayName)
	}
	if len(decoded.Tools[0].Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(decoded.Tools[0].Tags))
	}
}

func TestPayloadJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
	}{
		{
			name: "registered payload",
			payload: ServerRegisteredPayload{
				ServerID: "server-1",
				Name:     "alpha",
				SPIFFEID: "spiffe://cluster/ns/default/sa/alpha",
				Capabilities: types.Capability{
					Tools: []string{"tool.echo"},
				},
				Endpoint: "alpha.default.svc:8443",
			},
		},
		{
			name: "deregistered payload",
			payload: ServerDeregisteredPayload{
				ServerID: "server-1",
				Name:     "alpha",
				Reason:   "manual",
			},
		},
		{
			name: "health payload",
			payload: ServerHealthChangedPayload{
				ServerID:  "server-1",
				Name:      "alpha",
				OldStatus: types.StatusActive,
				NewStatus: types.StatusStale,
			},
		},
		{
			name: "policy payload",
			payload: PolicyViolatedPayload{
				ClientID:   "client-1",
				ToolName:   "danger.tool",
				Violations: []string{"denylist"},
				Decision:   "denied",
			},
		},
		{
			name: "server registered ack payload",
			payload: ServerRegisteredAckPayload{
				RegistrationID:  "server-1",
				LeaseEpoch:      2,
				CapabilityHash:  "hash-1",
				RegistryVersion: "vauto-1",
				ToolSchemaHash:  "schema-hash",
				AckedAt:         time.Date(2026, 2, 19, 3, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			out := map[string]any{}
			if err := json.Unmarshal(encoded, &out); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if len(out) == 0 {
				t.Fatal("expected non-empty payload json")
			}
		})
	}
}
